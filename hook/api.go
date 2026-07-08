// Package hook 提供宿主暴露给主题和插件脚本的统一扩展 API。
package hook

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"sort"
	"strings"

	"github.com/youthlin/wenlog/internal/store"

	gettext "github.com/youthlin/t"
)

// API 是暴露给插件脚本的宿主能力。
type API struct {
	log          *slog.Logger      // 打日志
	funcs        map[string]Func   // 保存注册的函数
	domain       string            // 翻译文本域
	dataLoader   *store.DataLoader // 数据获取
	ctx          context.Context   // 请求上下文
	options      []OptionDecl      // 主题的选项声明
	addAction    func(name string, fn any, priority ...int)
	addFilter    func(name string, fn any, priority ...int)
	removeAction func(name string)
	removeFilter func(name string)
}

// ========== ========== API 构造 ========== ==========

// NewAPI 创建一个 *API 实例
func NewAPI() *API {
	return &API{
		log:   slog.Default().With("component", "hook"),
		funcs: make(map[string]Func),
	}
}

func (api *API) WithDomain(domain string) *API {
	api.domain = domain
	return api
}

func (api *API) Domain() string { return api.domain }

// WithLoader 设置当前模板渲染请求的 DataLoader。
func (api *API) WithLoader(loader *store.DataLoader) *API {
	api.dataLoader = loader
	return api
}

// SetHookRegistrars 设置当前扩展 hook 注册函数，由宿主在加载插件或主题时注入。
func (api *API) SetHookRegistrars(addAction, addFilter func(name string, fn any, priority ...int)) *API {
	api.addAction = addAction
	api.addFilter = addFilter
	return api
}

// SetRemoveHooks 设置当前扩展 hook 移除函数，由宿主在加载插件或主题时注入。
func (api *API) SetRemoveHooks(removeAction, removeFilter func(name string)) *API {
	api.removeAction = removeAction
	api.removeFilter = removeFilter
	return api
}

// WithContext 返回绑定到当前请求 context 的 API 副本。
func (api *API) WithContext(ctx context.Context) *API {
	clone := *api
	clone.ctx = ctx
	if loader := getDataLoader(ctx); loader != nil {
		clone.dataLoader = loader
	}
	return &clone
}

// ========== ========== API 方法 ========== ==========

// AddAction
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
	return nil, false
}

// RegisterFunc 注册一个可在模板中通过 hookInvoke 调用的数据函数。
//
// 推荐签名为 func(api *hook.API, args hook.Args) any。为了降低迁移和编写门槛，
// 也兼容以下简化签名：
//
//   - func(args hook.Args) any
//   - func(api *hook.API) any
//   - func() any
func (api *API) RegisterFunc(name string, fn any) {
	wrapped := wrapTemplateFunc(fn)
	if api == nil || name == "" || wrapped == nil {
		return
	}
	api.funcs[name] = wrapped
}

func wrapTemplateFunc(fn any) Func {
	switch f := fn.(type) {
	case nil:
		return nil
	case Func: // = func(api *API, args Args) any
		return f
	case func(Args) any:
		return func(_ *API, args Args) any { return f(args) }
	case func(*API) any:
		return func(api *API, _ Args) any { return f(api) }
	case func() any:
		return func(_ *API, _ Args) any { return f() }
	}
	return nil
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
		slog.InfoContext(ctx, "InvokeFunc: function not found", "name", name, "available", api.FuncNames())
		return nil
	}
	defer func() { _ = recover() }()
	result := fn(api.WithContext(ctx), Args(args))
	slog.InfoContext(ctx, "InvokeFunc: done", "name", name, "result_nil", result == nil)
	return result
}

// AddFilter 注册 filter。插件可以使用 FilterFunc，也可以使用 func(any, ...any) any。
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
	_, _ = w.WriteString(fmt.Sprintf(format, args...))
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
	_, _ = w.WriteString(s)
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
func (api *API) WithThemeOptions(opts []OptionDecl) *API {
	api.options = opts
	return api
}

// GetOption 读取主题选项，未配置时回退到 theme.yaml 中的默认值。
func (api *API) GetOption(themeName, optionID string) string {
	loader := api.loader()
	if loader == nil || themeName == "" || optionID == "" {
		return ""
	}
	return GetOptionByID(func(key string) (string, error) {
		return loader.GetSetting(key), nil
	}, themeName, api.options, optionID)
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
