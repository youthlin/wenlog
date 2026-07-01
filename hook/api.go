// Package hook 提供宿主暴露给主题和插件脚本的统一扩展 API。
package hook

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"

	gettext "github.com/youthlin/t"
)

const (
	PriorityEarly   = 5
	PriorityDefault = 10
	PriorityLate    = 20
)

const (
	// HookHeadEnd 在主题 <head> 结束前触发，扩展可写入样式、脚本或 meta。
	HookHeadEnd = "head.end"
	// HookBodyEnd 在主题 </body> 前触发，扩展可写入延迟脚本。
	HookBodyEnd = "body.end"
	// HookCommentFormAfterTextarea 在评论表单 textarea 后触发，扩展可写入表情、附件等控件。
	HookCommentFormAfterTextarea = "comment.form.after_textarea"
	// HookWidgetRender 允许插件直接渲染自己的组件；未输出时回退到 widgets/<id>.gohtml。
	HookWidgetRender = "widget.render"
	// HookPostTitle 过滤文章/页面标题文本，运行于 postTitle 模板函数内部。
	HookPostTitle = "post.title"
	// HookPostExcerptHTML 过滤列表摘要 HTML，运行于 postExcerpt 模板函数内部。
	HookPostExcerptHTML = "post.excerpt_html"
	// HookPostContentHTML 过滤详情正文 HTML，运行于 postContent 模板函数内部。
	HookPostContentHTML = "post.content_html"
	// HookCommentContentHTML 过滤评论正文 HTML，运行于 commentContent 模板函数内部。
	HookCommentContentHTML = "comment.content_html"
	// HookWidgetRenderHTML 过滤单个组件渲染后的 HTML。
	HookWidgetRenderHTML = "widget.render_html"
	// HookHeadMeta 过滤 OpenGraph / Twitter Card meta 标签 HTML，运行于 headMeta 模板函数内部。
	// 插件可改写、追加或清空 meta 标签（返回空字符串即清除）。
	HookHeadMeta = "head.meta"
)

// actionWriterKey 用于在 context 中存储当前 action 的输出 writer。
type actionWriterKey struct{}

// withActionWriter 把输出 writer 注入 context，供 api.Printf 使用。
func withActionWriter(ctx context.Context, w io.StringWriter) context.Context {
	return context.WithValue(ctx, actionWriterKey{}, w)
}

// getActionWriter 从 context 中取出当前 action 的输出 writer。
func getActionWriter(ctx context.Context) io.StringWriter {
	if w, ok := ctx.Value(actionWriterKey{}).(io.StringWriter); ok {
		return w
	}
	return nil
}

// WithActionWriter 把输出 writer 注入 context，供 Registry.DoAction 内部使用。
func WithActionWriter(ctx context.Context, w io.StringWriter) context.Context {
	return withActionWriter(ctx, w)
}

// currentHookKey 用于在 context 中存储当前正在执行的 hook 名称。
type currentHookKey struct{}

// WithCurrentHook 把当前 hook 名注入 context。
func WithCurrentHook(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, currentHookKey{}, name)
}

// CurrentHook 从 context 中读取当前正在执行的 hook 名称。
func CurrentHook(ctx context.Context) string {
	if s, ok := ctx.Value(currentHookKey{}).(string); ok {
		return s
	}
	return ""
}

// ---- 各 hook 点的结构化参数类型 ----
// 插件 handler 可以直接用这些类型作为第二个参数，无需按位置从 args 中取。

// HeadEndData 是 head.end action 的参数。
type HeadEndData struct {
	Data any // 模板数据（. dot）
}

// BodyEndData 是 body.end action 的参数。
type BodyEndData struct {
	Data any
}

// CommentFormAfterTextareaData 是 comment.form.after_textarea action 的参数。
type CommentFormAfterTextareaData struct {
	Data any
}

// ActionFunc 是插件注册 action 时推荐使用的函数签名。
type ActionFunc = func(api *API, args ...any)

// FilterFunc 是插件注册 filter 时推荐使用的函数签名。
type FilterFunc = func(api *API, value any, args ...any) any

// Args 是模板通过 hookInvoke 传给扩展函数的命名参数。
//
// 主题/插件作者可以直接用：
//
//	api.RegisterFunc("latest", func(api *hook.API, args hook.Args) any {
//		return api.RecentPosts(args.Int("count", 5))
//	})
//
// 比起在每个扩展里重复写类型断言，这些小工具让 functions.goyaegi 的主流程更接近业务描述。
type Args map[string]any

// Any 返回原始参数值；不存在时返回 nil。
func (a Args) Any(key string) any { return a[key] }

// String 读取字符串参数；空字符串或不存在时返回 def。
func (a Args) String(key, def string) string {
	if s, ok := a[key].(string); ok && s != "" {
		return s
	}
	return def
}

// Int 读取整数参数；支持常见整型、浮点数和数字字符串。
func (a Args) Int(key string, def int) int {
	switch v := a[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case uint:
		return int(v)
	case uint64:
		return int(v)
	case uint32:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// PositiveInt 读取正整数参数；不存在、无法转换或小于等于 0 时返回 def。
func (a Args) PositiveInt(key string, def int) int {
	if n := a.Int(key, def); n > 0 {
		return n
	}
	return def
}

// Bool 读取布尔参数；支持 bool 和 strconv.ParseBool 可识别的字符串。
func (a Args) Bool(key string, def bool) bool {
	switch v := a[key].(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

// Func 是插件注册给模板调用的数据函数。
type Func func(api *API, args Args) any

// WidgetRenderContext 是 widget.render action 接收的插件组件渲染上下文。
type WidgetRenderContext struct {
	PluginID string
	WidgetID string
	Options  map[string]string
	Data     any
}

// SelectOpt 是 select 类型选项的一个可选项。
type SelectOpt struct {
	Value string `yaml:"value" json:"value"`
	Label string `yaml:"label" json:"label"`
}

// Source 记录一个 hook 处理器来自核心、主题还是插件。
type Source struct {
	Type string // core / theme / plugin
	ID   string // default / twentytwenty / saying
}

// WidgetDecl 描述主题或插件声明的一个可用组件。
type WidgetDecl struct {
	ID       string       `yaml:"id" json:"id"`
	Label    string       `yaml:"label" json:"label"`
	Options  []OptionDecl `yaml:"options,omitempty" json:"options,omitempty"`
	Source   string       `yaml:"-" json:"source,omitempty"`
	PluginID string       `yaml:"-" json:"plugin_id,omitempty"`
}
type WidgetDeclProvider = func(ctx context.Context) []WidgetDecl

// WidgetInstance 表示一个组件实例的运行时状态，包含实例 ID 和配置值。
// 同一组件类型可以在同一区域添加多次，每次有独立的 InstanceID 和 Settings。
type WidgetInstance struct {
	InstanceID string            // 实例唯一 ID
	WidgetID   string            // 组件类型 ID
	Settings   map[string]string // 实例级配置值
}

// WidgetResolver 根据来源和 ID 查找组件实现。
// source 为 "builtin" / "theme" / "plugin"，pluginID 仅在 source="plugin" 时有值
type WidgetResolver = func(source, id, pluginID string) Widget

// Widget 是所有组件（内置/主题/插件）的统一接口。
// 参照 WordPress WP_Widget 的模板方法模式：核心管理生命周期和存储，
// 组件实现者只需提供元数据和渲染逻辑。
type Widget interface {
	// Meta 返回组件声明元数据（ID、标签、可配置选项等）。
	Meta() WidgetDecl
	// Render 渲染组件为 HTML。tpl 是当前请求的主题模板实例，模板型组件用它执行模板；
	// action 型组件可忽略 tpl 参数。
	Render(ctx context.Context, tpl *template.Template, instance WidgetInstance, data any) (template.HTML, error)
}

// SettingStore 是主题/插件管理器需要的设置存储接口。
type SettingStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

// Translatable 表示可加载翻译的资源（主题或插件）。
type Translatable interface {
	LoadTranslations() error
}

// LoadTranslations 遍历列表加载所有资源的翻译。
func LoadTranslations[T Translatable](items []T) error {
	for _, item := range items {
		if err := item.LoadTranslations(); err != nil {
			return err
		}
	}
	return nil
}

// OptionDecl 描述主题或扩展声明的一个可配置选项。
type OptionDecl struct {
	ID          string      `yaml:"id" json:"id"`
	Type        string      `yaml:"type" json:"type"`
	Label       string      `yaml:"label" json:"label"`
	Description string      `yaml:"description" json:"description"`
	Default     string      `yaml:"default" json:"default"`
	Min         *float64    `yaml:"min" json:"min,omitempty"`
	Max         *float64    `yaml:"max" json:"max,omitempty"`
	Options     []SelectOpt `yaml:"options" json:"options,omitempty"`
}

// API 是暴露给插件脚本的宿主能力。
type API struct {
	addAction    func(name string, fn any, priority ...int)
	addFilter    func(name string, fn any, priority ...int)
	removeAction func(name string)
	removeFilter func(name string)
	funcs        map[string]Func
	ctx          context.Context
	domain       string
	dataLoader   *store.DataLoader
	options      []OptionDecl
}

// New 创建插件 API。
func New(addAction, addFilter func(name string, fn any, priority ...int), domain string) *API {
	return &API{addAction: addAction, addFilter: addFilter, funcs: make(map[string]Func), domain: domain}
}

// NewWithLoader 创建一个直接绑定只读 DataLoader 的扩展 API，主要供主题脚本使用。
func NewWithLoader(loader *store.DataLoader, domain string) *API {
	return &API{dataLoader: loader, funcs: make(map[string]Func), domain: domain}
}

// SetLoader 设置当前模板渲染请求的 DataLoader。
func (api *API) SetLoader(loader *store.DataLoader) {
	if api == nil {
		return
	}
	api.dataLoader = loader
}

// SetHookRegistrars 设置当前扩展 hook 注册函数，由宿主在加载插件或主题时注入。
func (api *API) SetHookRegistrars(addAction, addFilter func(name string, fn any, priority ...int)) {
	if api == nil {
		return
	}
	api.addAction = addAction
	api.addFilter = addFilter
}

// SetRemoveHooks 设置当前扩展 hook 移除函数，由宿主在加载插件或主题时注入。
func (api *API) SetRemoveHooks(removeAction, removeFilter func(name string)) {
	if api == nil {
		return
	}
	api.removeAction = removeAction
	api.removeFilter = removeFilter
}

// WithContext 返回绑定到当前请求 context 的 API 副本。
func (api *API) WithContext(ctx context.Context) *API {
	if api == nil {
		return nil
	}
	clone := *api
	clone.ctx = ctx
	if ctx != nil {
		if loader, _ := ctx.Value(loaderContextKey{}).(*store.DataLoader); loader != nil {
			clone.dataLoader = loader
		}
	}
	return &clone
}

// RegisterFunc 注册一个可在模板中通过 hookInvoke 调用的数据函数。
//
// 推荐签名为 func(api *hook.API, args hook.Args) any。为了降低迁移和编写门槛，
// 也兼容以下简化签名：
//
//   - func(args hook.Args) any
//   - func(api *hook.API) any
//   - func() any
//   - 旧签名 func(api *hook.API, args map[string]any) any
func (api *API) RegisterFunc(name string, fn any) {
	wrapped := wrapTemplateFunc(fn)
	if api == nil || name == "" || wrapped == nil {
		return
	}
	api.funcs[name] = wrapped
}

// GetFunc 获取已注册的命名函数。
func (api *API) GetFunc(name string) Func {
	if api == nil || name == "" {
		return nil
	}
	return api.funcs[name]
}

// FuncNames 返回所有已注册命名函数名称。
func (api *API) FuncNames() []string {
	if api == nil {
		return nil
	}
	names := make([]string, 0, len(api.funcs))
	for name := range api.funcs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// InvokeFunc 调用已注册的数据函数。
func (api *API) InvokeFunc(ctx context.Context, name string, args map[string]any) any {
	if api == nil || name == "" {
		return nil
	}
	fn := api.funcs[name]
	if fn == nil {
		slog.Info("InvokeFunc: function not found", "name", name, "available", api.FuncNames())
		return nil
	}
	defer func() { _ = recover() }()
	result := fn(api.WithContext(ctx), Args(args))
	slog.Info("InvokeFunc: done", "name", name, "result_nil", result == nil)
	return result
}

func wrapTemplateFunc(fn any) Func {
	switch f := fn.(type) {
	case nil:
		return nil
	case Func:
		return f
	case func(*API, Args) any:
		return f
	case func(*API, map[string]any) any:
		return func(api *API, args Args) any { return f(api, map[string]any(args)) }
	case func(Args) any:
		return func(_ *API, args Args) any { return f(args) }
	case func(map[string]any) any:
		return func(_ *API, args Args) any { return f(map[string]any(args)) }
	case func(*API) any:
		return func(api *API, _ Args) any { return f(api) }
	case func() any:
		return func(_ *API, _ Args) any { return f() }
	}
	return nil
}

// AddAction 注册 action。插件可以使用 ActionFunc，也可以使用 func(...any) / func()。
func (api *API) AddAction(name string, fn any, priority ...int) {
	if api == nil || api.addAction == nil || name == "" || fn == nil {
		return
	}
	if wrapped, ok := api.wrapAction(fn); ok {
		api.addAction(name, wrapped, priority...)
		return
	}
	api.addAction(name, fn, priority...)
}

func (api *API) wrapAction(fn any) (func(context.Context, ...any), bool) {
	switch f := fn.(type) {
	case ActionFunc:
		return func(ctx context.Context, args ...any) { f(api.WithContext(ctx), args...) }, true
	case func(*API):
		return func(ctx context.Context, _ ...any) { f(api.WithContext(ctx)) }, true
	}
	// 支持 func(*API, SomeTypedData) 签名，通过反射在注册时生成高效包装。
	if wrapped, ok := api.wrapTypedAction(fn); ok {
		return wrapped, true
	}
	return nil, false
}

// wrapTypedAction 将 func(*API, T) 包装为 func(context.Context, ...any)。
// T 是各 hook 的结构化参数类型（如 HeadEndData）。
func (api *API) wrapTypedAction(fn any) (func(context.Context, ...any), bool) {
	rv := reflect.ValueOf(fn)
	if !rv.IsValid() || rv.Kind() != reflect.Func {
		return nil, false
	}
	rt := rv.Type()
	if rt.NumIn() != 2 || rt.NumOut() != 0 {
		return nil, false
	}
	if rt.In(0) != reflect.TypeOf((*API)(nil)) {
		return nil, false
	}
	dataType := rt.In(1)
	return func(ctx context.Context, args ...any) {
		var data reflect.Value
		if len(args) > 0 && args[0] != nil {
			rvArg := reflect.ValueOf(args[0])
			if rvArg.Type().AssignableTo(dataType) {
				data = rvArg
			}
		}
		if !data.IsValid() {
			data = reflect.Zero(dataType)
		}
		rv.Call([]reflect.Value{reflect.ValueOf(api.WithContext(ctx)), data})
	}, true
}

// AddFilter 注册 filter。插件可以使用 FilterFunc，也可以使用 func(any, ...any) any / func(any) any。
func (api *API) AddFilter(name string, fn any, priority ...int) {
	if api == nil || api.addFilter == nil || name == "" || fn == nil {
		return
	}
	if wrapped, ok := api.wrapFilter(fn); ok {
		api.addFilter(name, wrapped, priority...)
		return
	}
	api.addFilter(name, fn, priority...)
}

func (api *API) wrapFilter(fn any) (func(context.Context, any, ...any) any, bool) {
	switch f := fn.(type) {
	case FilterFunc:
		return func(ctx context.Context, value any, args ...any) any { return f(api.WithContext(ctx), value, args...) }, true
	case func(*API, any) any:
		return func(ctx context.Context, value any, _ ...any) any { return f(api.WithContext(ctx), value) }, true
	case func(*API) any:
		return func(ctx context.Context, _ any, _ ...any) any { return f(api.WithContext(ctx)) }, true
	}
	return nil, false
}

// Printf 向当前 action 的输出 writer 写入格式化字符串。
// 仅在 action handler 内部调用有效；filter 中无 writer 注入。
func (api *API) Printf(format string, args ...any) {
	if api == nil || api.ctx == nil {
		return
	}
	w := getActionWriter(api.ctx)
	if w == nil {
		return
	}
	w.WriteString(fmt.Sprintf(format, args...))
}

// Print 向当前 action 的输出 writer 写入原始字符串。
func (api *API) Print(s string) {
	if api == nil || api.ctx == nil {
		return
	}
	w := getActionWriter(api.ctx)
	if w == nil {
		return
	}
	w.WriteString(s)
}

// RemoveAction 移除当前扩展注册的所有同名 action。
func (api *API) RemoveAction(name string) {
	if api == nil || api.removeAction == nil || name == "" {
		return
	}
	api.removeAction(name)
}

// RemoveFilter 移除当前扩展注册的所有同名 filter。
func (api *API) RemoveFilter(name string) {
	if api == nil || api.removeFilter == nil || name == "" {
		return
	}
	api.removeFilter(name)
}

// T 翻译普通消息。
func (api *API) T(msg string, args ...any) string {
	return api.translator().T(msg, args...)
}

// N 按数量翻译单复数消息。
func (api *API) N(singular, plural string, n int, args ...any) string {
	return api.translator().N(singular, plural, n, args...)
}

// X 按上下文翻译消息。
func (api *API) X(ctx, msg string, args ...any) string {
	return api.translator().X(ctx, msg, args...)
}

// XN 按上下文和数量翻译单复数消息。
func (api *API) XN(ctx, singular, plural string, n int, args ...any) string {
	return api.translator().XN(ctx, singular, plural, n, args...)
}

func (api *API) translator() *gettext.Translations {
	if api == nil {
		return gettext.Global()
	}
	tr := gettext.Global()
	if api.ctx != nil {
		tr = gettext.WithContext(api.ctx)
	}
	if api.domain != "" {
		tr = tr.D(api.domain)
	}
	return tr
}

// EscapeHTML 转义 HTML 特殊字符。
func (api *API) EscapeHTML(s string) string { return html.EscapeString(s) }

// WriteString 向宿主传入的输出对象写入字符串。
// 插件脚本运行在解释器内，直接做接口断言可能受类型边界影响，因此由宿主侧统一完成写入。
func (api *API) WriteString(w any, s string) {
	if w == nil || s == "" {
		return
	}
	if out, ok := w.(interface{ WriteString(string) (int, error) }); ok {
		_, _ = out.WriteString(s)
	}
}

// PluginOption 读取插件全局选项，未配置时返回 def。
func (api *API) PluginOption(optionID, def string) string {
	loader := api.loader()
	if loader == nil || optionID == "" {
		return def
	}
	pluginID := strings.TrimPrefix(api.domain, "plugin_")
	if pluginID == "" || pluginID == api.domain {
		return def
	}
	value := loader.GetSetting("plugin_" + pluginID + "_" + optionID)
	if value == "" {
		return def
	}
	return value
}

// Setting 读取站点设置项。
func (api *API) Setting(key string) string {
	loader := api.loader()
	if loader == nil || key == "" {
		return ""
	}
	return loader.GetSetting(key)
}

// Settings 批量读取站点设置项。
func (api *API) Settings(keys ...string) map[string]string {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	return loader.GetSettings(keys...)
}

// SetThemeOptions 设置主题声明的选项列表（含默认值），供 GetOption 回退使用。
func (api *API) SetThemeOptions(opts []OptionDecl) {
	if api == nil {
		return
	}
	api.options = opts
}

// GetOption 读取主题选项，未配置时回退到 theme.yaml 中的默认值。
func (api *API) GetOption(themeName, optionID string) string {
	loader := api.loader()
	if loader == nil || themeName == "" || optionID == "" {
		return ""
	}
	return GetOptionByID(func(key string) (string, error) { return loader.GetSetting(key), nil }, themeName, api.options, optionID)
}

// Posts 返回全部已发布文章（按发布时间倒序）。
func (api *API) Posts() []PostView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	posts := loader.PostsByType(model.PostTypePost)
	result := make([]PostView, 0, len(posts))
	for _, p := range posts {
		if v := toPostView(p, loader); v != nil {
			result = append(result, *v)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PublishedAt.After(result[j].PublishedAt) })
	return result
}

// Post 按 ID 返回文章或页面。
func (api *API) Post(postID any) *PostView {
	loader := api.loader()
	id := toUint(postID)
	if id == 0 || loader == nil {
		return nil
	}
	return toPostView(loader.Posts[id], loader)
}

// Pages 返回全部已发布页面。
func (api *API) Pages() []PostView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	pages := loader.PostsByType(model.PostTypePage)
	result := make([]PostView, 0, len(pages))
	for _, p := range pages {
		if v := toPostView(p, loader); v != nil {
			result = append(result, *v)
		}
	}
	return result
}

// PageBySlug 按 slug 获取页面。
func (api *API) PageBySlug(slug string) *PostView {
	loader := api.loader()
	if loader == nil || slug == "" {
		return nil
	}
	return toPostView(loader.GetPageBySlug(slug), loader)
}

// RecentPosts 返回最近 n 篇文章。
func (api *API) RecentPosts(n int) []PostView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	posts := loader.RecentPosts(n)
	result := make([]PostView, 0, len(posts))
	for i := range posts {
		if v := toPostView(&posts[i], loader); v != nil {
			result = append(result, *v)
		}
	}
	return result
}

// PostsByCategory 返回指定分类下的文章。
func (api *API) PostsByCategory(categorySlug string) []PostView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	res := loader.ListPosts(1, 10000, categorySlug, "")
	result := make([]PostView, 0, len(res.Posts))
	for i := range res.Posts {
		if v := toPostView(&res.Posts[i], loader); v != nil {
			result = append(result, *v)
		}
	}
	return result
}

// PostsByTag 返回指定标签下的文章。
func (api *API) PostsByTag(tagSlug string) []PostView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	res := loader.ListPosts(1, 10000, "", tagSlug)
	result := make([]PostView, 0, len(res.Posts))
	for i := range res.Posts {
		if v := toPostView(&res.Posts[i], loader); v != nil {
			result = append(result, *v)
		}
	}
	return result
}

// PostsByYear 返回指定年份的文章。
func (api *API) PostsByYear(year int) []PostView {
	all := api.Posts()
	result := make([]PostView, 0)
	for _, p := range all {
		if p.PublishedAt.Year() == year {
			result = append(result, p)
		}
	}
	return result
}

// PostsByYearMonth 返回指定年月的文章。
func (api *API) PostsByYearMonth(year, month int) []PostView {
	all := api.Posts()
	result := make([]PostView, 0)
	for _, p := range all {
		if p.PublishedAt.Year() == year && int(p.PublishedAt.Month()) == month {
			result = append(result, p)
		}
	}
	return result
}

// Categories 返回全部分类。
func (api *API) Categories() []CategoryView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	cats := loader.AllCategories()
	result := make([]CategoryView, 0, len(cats))
	for i := range cats {
		result = append(result, toCategoryView(&cats[i]))
	}
	return result
}

// Tags 返回全部标签。
func (api *API) Tags() []TagView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	tags := loader.AllTags()
	result := make([]TagView, 0, len(tags))
	for i := range tags {
		result = append(result, toTagView(&tags[i]))
	}
	return result
}

// CommentsByPost 返回指定文章的已批准评论。
func (api *API) CommentsByPost(postID any) []CommentView {
	loader := api.loader()
	id := toUint(postID)
	if loader == nil || id == 0 {
		return nil
	}
	commentIDs := loader.CommentIDsByPost(id)
	result := make([]CommentView, 0, len(commentIDs))
	for _, cid := range commentIDs {
		if c, ok := loader.Comments[cid]; ok {
			result = append(result, toCommentView(c))
		}
	}
	return result
}

// RecentComments 返回最近 n 条已批准评论。
func (api *API) RecentComments(n int) []CommentView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	comments := loader.RecentComments(n)
	result := make([]CommentView, 0, len(comments))
	for i := range comments {
		result = append(result, toCommentView(&comments[i]))
	}
	return result
}

// Users 返回全部用户。
func (api *API) Users() []UserView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	result := make([]UserView, 0, len(loader.Users))
	for _, u := range loader.Users {
		if v := toUserView(u); v != nil {
			result = append(result, *v)
		}
	}
	return result
}

// User 按 ID 返回用户。
func (api *API) User(userID any) *UserView {
	loader := api.loader()
	id := toUint(userID)
	if id == 0 || loader == nil {
		return nil
	}
	return toUserView(loader.Users[id])
}

// ArchiveMonths 返回归档月份统计。
func (api *API) ArchiveMonths() []ArchiveMonthView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	months := loader.ArchiveMonths()
	result := make([]ArchiveMonthView, 0, len(months))
	for _, m := range months {
		result = append(result, ArchiveMonthView{Year: m.Year, Month: m.Month, Count: m.Count})
	}
	return result
}

// PostURL 生成文章永久链接。
func (api *API) PostURL(post any) string {
	p := postModel(post)
	if p == nil {
		return ""
	}
	return permalink.Post(viewPostModel(p))
}

// PageURL 生成页面永久链接。
func (api *API) PageURL(post any) string {
	p := postModel(post)
	if p == nil {
		return ""
	}
	return permalink.Page(viewPostModel(p))
}

func toUint(v any) uint {
	switch n := v.(type) {
	case uint:
		return n
	case uint64:
		return uint(n)
	case uint32:
		return uint(n)
	case int:
		if n > 0 {
			return uint(n)
		}
	case int64:
		if n > 0 {
			return uint(n)
		}
	case int32:
		if n > 0 {
			return uint(n)
		}
	case string:
		parsed, _ := strconv.ParseUint(strings.TrimSpace(n), 10, 64)
		return uint(parsed)
	}
	return 0
}

func postModel(v any) *PostView {
	switch p := v.(type) {
	case *PostView:
		return p
	case PostView:
		return &p
	default:
		return nil
	}
}

func viewPostModel(p *PostView) *model.Post {
	if p == nil {
		return nil
	}
	return &model.Post{ID: p.ID, Title: p.Title, Slug: p.Slug, PostType: p.PostType, Status: p.Status, PublishedAt: p.PublishedAt, ModifiedAt: p.ModifiedAt}
}

func toPostView(p *model.Post, loader *store.DataLoader) *PostView {
	if p == nil {
		return nil
	}
	v := &PostView{
		ID:           p.ID,
		Title:        p.Title,
		Slug:         p.Slug,
		Excerpt:      p.Excerpt,
		Content:      p.Content,
		AuthorID:     p.AuthorID,
		Status:       p.Status,
		PostType:     p.PostType,
		Views:        p.Views,
		MenuOrder:    p.MenuOrder,
		PublishedAt:  p.PublishedAt,
		ModifiedAt:   p.ModifiedAt,
		CommentCount: p.CommentCount,
	}
	if loader != nil {
		if u, ok := loader.Users[p.AuthorID]; ok {
			if author := toUserView(u); author != nil {
				v.Author = *author
			}
		}
		for _, cid := range loader.PostCategoryIDs(p.ID) {
			if c, ok := loader.Categories[cid]; ok {
				v.Categories = append(v.Categories, toCategoryView(c))
			}
		}
		for _, tid := range loader.PostTagIDs(p.ID) {
			if t, ok := loader.Tags[tid]; ok {
				v.Tags = append(v.Tags, toTagView(t))
			}
		}
	}
	return v
}

func toCategoryView(c *model.Category) CategoryView {
	if c == nil {
		return CategoryView{}
	}
	return CategoryView{ID: c.ID, Name: c.Name, Slug: c.Slug, Description: c.Description, ParentID: c.ParentID, PostCount: c.PostCount}
}

func toTagView(t *model.Tag) TagView {
	if t == nil {
		return TagView{}
	}
	return TagView{ID: t.ID, Name: t.Name, Slug: t.Slug, PostCount: t.PostCount}
}

func toCommentView(c *model.Comment) CommentView {
	if c == nil {
		return CommentView{}
	}
	return CommentView{
		ID:            c.ID,
		PostID:        c.PostID,
		ParentID:      c.ParentID,
		ReplyToID:     c.ReplyToID,
		UserID:        c.UserID,
		Author:        c.Author,
		Email:         c.Email,
		URL:           c.URL,
		IP:            c.IP,
		Content:       c.Content,
		Status:        c.Status,
		NotifyOnReply: c.NotifyOnReply,
		CreatedAt:     c.CreatedAt,
		ReplyToAuthor: c.ReplyToAuthor,
		CommenterRole: c.CommenterRole,
	}
}

func toUserView(u *model.User) *UserView {
	if u == nil {
		return nil
	}
	return &UserView{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, Website: u.Website, Role: u.Role}
}

// CommentURL 生成评论锚点链接，评论不在第一页时自动拼接 cpage 参数。
func (api *API) CommentURL(post any, comment any) string {
	base := ""
	var pv *PostView
	if pv = postModel(post); pv != nil {
		if pv.PostType == model.PostTypePage {
			base = api.PageURL(pv)
		} else {
			base = api.PostURL(pv)
		}
	}
	cid := commentID(comment)
	if pv != nil {
		if loader := api.loader(); loader != nil {
			if page := loader.CommentPageForID(pv.ID, cid, 20); page > 1 {
				return base + "?cpage=" + strconv.Itoa(page) + "#comment-" + strconv.Itoa(int(cid))
			}
		}
	}
	return base + "#comment-" + strconv.Itoa(int(cid))
}

// CategoryURL 生成分类永久链接。
func (api *API) CategoryURL(slug string) string { return permalink.Category(slug) }

// TagURL 生成标签永久链接。
func (api *API) TagURL(slug string) string { return permalink.Tag(slug) }

func commentID(v any) uint {
	switch c := v.(type) {
	case CommentView:
		return c.ID
	case *CommentView:
		if c != nil {
			return c.ID
		}
	}
	return toUint(v)
}

// Snippet 截取一段文本摘要。
func (api *API) Snippet(content any, n int) string {
	if n <= 0 {
		n = 36
	}
	runes := []rune(fmt.Sprint(content))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n]) + "…"
}

// AvatarURL 由邮箱生成 cravatar(国内镜像)头像 URL。
func (api *API) AvatarURL(email, defaultAvatar string) string {
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email))))
	hash := hex.EncodeToString(sum[:])
	return "https://cn.cravatar.com/avatar/" + hash + "?s=" + strconv.Itoa(consts.AvatarSizeSmall) + "&d=" + util.NormalizeDefaultAvatar(defaultAvatar)
}

// OptionKey 返回 option 在 Setting 表中的 key。
func OptionKey(themeName, optionID string) string {
	return fmt.Sprintf("option_%s_%s", themeName, optionID)
}

// GetOption 从 Setting 表读取 option 值，未配置时返回 default 值。
func GetOption(getSetting func(key string) (string, error), themeName string, opt OptionDecl) string {
	key := OptionKey(themeName, opt.ID)
	val, err := getSetting(key)
	if err != nil || val == "" {
		return opt.Default
	}
	return val
}

// GetOptionByID 从选项声明列表中查找指定 option 并读取其配置值。
func GetOptionByID(getSetting func(key string) (string, error), themeName string, options []OptionDecl, optionID string) string {
	for _, opt := range options {
		if opt.ID == optionID {
			return GetOption(getSetting, themeName, opt)
		}
	}
	return ""
}

func (api *API) loader() *store.DataLoader {
	if api == nil {
		return nil
	}
	if api.ctx != nil {
		if loader, _ := api.ctx.Value(loaderContextKey{}).(*store.DataLoader); loader != nil {
			return loader
		}
	}
	return api.dataLoader
}
