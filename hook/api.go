// Package hook 提供宿主暴露给主题和插件脚本的统一扩展 API。
package hook

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"html"
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
)

// ActionFunc 是插件注册 action 时推荐使用的函数签名。
type ActionFunc = func(api *API, args ...any)

// FilterFunc 是插件注册 filter 时推荐使用的函数签名。
type FilterFunc = func(api *API, value any, args ...any) any

// Func 是插件注册给模板调用的数据函数。
type Func = func(api *API, args map[string]any) any

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

type loaderContextKey struct{}

// WithDataLoader 把当前请求的只读数据加载器绑定到 context，供插件 API 读取。
func WithDataLoader(ctx context.Context, loader *store.DataLoader) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if loader == nil {
		return ctx
	}
	return context.WithValue(ctx, loaderContextKey{}, loader)
}

// API 是暴露给插件脚本的宿主能力。
type API struct {
	addAction  func(name string, fn any, priority ...int)
	addFilter  func(name string, fn any, priority ...int)
	funcs      map[string]Func
	ctx        context.Context
	domain     string
	dataLoader *store.DataLoader
	options    []OptionDecl
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
func (api *API) RegisterFunc(name string, fn Func) {
	if api == nil || name == "" || fn == nil {
		return
	}
	api.funcs[name] = fn
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
		return nil
	}
	defer func() { _ = recover() }()
	return fn(api.WithContext(ctx), args)
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
	}
	if rv := reflect.ValueOf(fn); pluginAPIFunc(rv) {
		return func(ctx context.Context, args ...any) { callPluginAction(rv, api.WithContext(ctx), args...) }, true
	}
	return nil, false
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
	}
	if rv := reflect.ValueOf(fn); pluginAPIFunc(rv) {
		return func(ctx context.Context, value any, args ...any) any {
			return callPluginFilter(rv, api.WithContext(ctx), value, args...)
		}, true
	}
	return nil, false
}

var apiType = reflect.TypeOf((*API)(nil))

func pluginAPIFunc(rv reflect.Value) bool {
	if !rv.IsValid() || rv.Kind() != reflect.Func || rv.Type().NumIn() == 0 {
		return false
	}
	first := rv.Type().In(0)
	return apiType.AssignableTo(first) || apiType.ConvertibleTo(first)
}

func callPluginAction(fn reflect.Value, api *API, args ...any) {
	callPluginFunc(fn, append([]any{api}, args...)...)
}

func callPluginFilter(fn reflect.Value, api *API, value any, args ...any) any {
	outs := callPluginFunc(fn, append([]any{api, value}, args...)...)
	if len(outs) == 0 || !outs[0].IsValid() || !outs[0].CanInterface() {
		return value
	}
	return outs[0].Interface()
}

func callPluginFunc(fn reflect.Value, args ...any) []reflect.Value {
	rt := fn.Type()
	if !rt.IsVariadic() && len(args) != rt.NumIn() {
		return nil
	}
	if rt.IsVariadic() && len(args) < rt.NumIn()-1 {
		return nil
	}
	in := make([]reflect.Value, 0, len(args))
	for i, arg := range args {
		argType := rt.In(i)
		if rt.IsVariadic() && i >= rt.NumIn()-1 {
			argType = rt.In(rt.NumIn() - 1).Elem()
		}
		v, ok := pluginReflectArg(arg, argType)
		if !ok {
			return nil
		}
		in = append(in, v)
	}
	return fn.Call(in)
}

func pluginReflectArg(v any, typ reflect.Type) (reflect.Value, bool) {
	if v == nil {
		return reflect.Zero(typ), true
	}
	rv := reflect.ValueOf(v)
	if rv.Type().AssignableTo(typ) {
		return rv, true
	}
	if rv.Type().ConvertibleTo(typ) {
		return rv.Convert(typ), true
	}
	return reflect.Value{}, false
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

// SayingComments 返回指定文章下博主本人的评论，插件可基于这些基础数据自行构造模板视图。
func (api *API) SayingComments(postID any, n int) []CommentView {
	loader := api.loader()
	id := toUint(postID)
	if id == 0 || loader == nil {
		return nil
	}
	comments := loader.SayingComments(id, n)
	result := make([]CommentView, 0, len(comments))
	for i := range comments {
		result = append(result, toCommentView(&comments[i]))
	}
	return result
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
	return CommentView{ID: c.ID, PostID: c.PostID, ParentID: c.ParentID, Author: c.Author, Email: c.Email, URL: c.URL, Content: c.Content, Status: c.Status, CreatedAt: c.CreatedAt}
}

func toUserView(u *model.User) *UserView {
	if u == nil {
		return nil
	}
	return &UserView{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, Website: u.Website, Role: u.Role}
}

// CommentURL 生成评论锚点链接。
func (api *API) CommentURL(post any, comment any) string {
	base := ""
	if p := postModel(post); p != nil {
		if p.PostType == model.PostTypePage {
			base = api.PageURL(p)
		} else {
			base = api.PostURL(p)
		}
	}
	return base + "#comment-" + strconv.Itoa(int(commentID(comment)))
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

func commentSnippet(content string) string {
	runes := []rune(content)
	if len(runes) <= 36 {
		return content
	}
	return string(runes[:36]) + "…"
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
