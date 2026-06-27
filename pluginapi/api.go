// Package pluginapi 提供宿主暴露给插件脚本的最小 API。
package pluginapi

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"html"
	"reflect"
	"strconv"
	"strings"
	"time"

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

// Api 是当前注入到插件脚本运行环境中的宿主 API 实例。
var Api *API

// ActionFunc 是插件注册 action 时推荐使用的函数签名。
type ActionFunc func(api *API, args ...any)

// FilterFunc 是插件注册 filter 时推荐使用的函数签名。
type FilterFunc func(api *API, value any, args ...any) any

// Func 是插件注册给模板调用的数据函数。
type Func func(api *API, args map[string]any) any

// WidgetRenderContext 是 widget.render action 接收的插件组件渲染上下文。
type WidgetRenderContext struct {
	PluginID string
	WidgetID string
	Options  map[string]string
	Data     any
}

// PostView 是插件 API 暴露的文章/页面只读视图。
type PostView struct {
	ID          uint
	Title       string
	Slug        string
	AuthorID    uint
	PostType    string
	PublishedAt time.Time
}

// CommentView 是插件 API 暴露的评论只读视图。
type CommentView struct {
	ID        uint
	PostID    uint
	ParentID  uint
	Author    string
	Email     string
	URL       string
	Content   string
	Status    string
	CreatedAt any
}

// UserView 是插件 API 暴露的用户只读视图。
type UserView struct {
	ID          uint
	Username    string
	DisplayName string
	Email       string
	Website     string
	Role        string
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
	addAction func(name string, fn any, priority ...int)
	addFilter func(name string, fn any, priority ...int)
	funcs     map[string]any
	ctx       context.Context
	domain    string
}

// New 创建插件 API。
func New(addAction, addFilter func(name string, fn any, priority ...int), domain string) *API {
	return &API{addAction: addAction, addFilter: addFilter, funcs: make(map[string]any), domain: domain}
}

// WithContext 返回绑定到当前请求 context 的 API 副本。
func (api *API) WithContext(ctx context.Context) *API {
	if api == nil {
		return nil
	}
	clone := *api
	clone.ctx = ctx
	return &clone
}

// RegisterFunc 注册一个可在插件模板中通过 pluginInvoke 调用的数据函数。
func (api *API) RegisterFunc(name string, fn any) {
	if api == nil || name == "" || fn == nil {
		return
	}
	api.funcs[name] = fn
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
	requestAPI := api.WithContext(ctx)
	defer func() { _ = recover() }()
	switch f := fn.(type) {
	case Func:
		return f(requestAPI, args)
	case func(*API, map[string]any) any:
		return f(requestAPI, args)
	case func(map[string]any) any:
		return f(args)
	case func() any:
		return f()
	default:
		rv := reflect.ValueOf(fn)
		if !rv.IsValid() || rv.Kind() != reflect.Func {
			return nil
		}
		outs := callPluginFunc(rv, requestAPI, args)
		if len(outs) == 0 || !outs[0].IsValid() || !outs[0].CanInterface() {
			return nil
		}
		return outs[0].Interface()
	}
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
	case func(*API, ...any):
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
	case func(*API, any, ...any) any:
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

// Post 按 ID 返回文章或页面。
func (api *API) Post(postID any) *PostView {
	loader := api.loader()
	id := toUint(postID)
	if id == 0 || loader == nil {
		return nil
	}
	return toPostView(loader.Posts[id])
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
	return &model.Post{ID: p.ID, Title: p.Title, Slug: p.Slug, PostType: p.PostType, PublishedAt: p.PublishedAt}
}

func toPostView(p *model.Post) *PostView {
	if p == nil {
		return nil
	}
	return &PostView{ID: p.ID, Title: p.Title, Slug: p.Slug, AuthorID: p.AuthorID, PostType: p.PostType, PublishedAt: p.PublishedAt}
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

func (api *API) loader() *store.DataLoader {
	if api == nil || api.ctx == nil {
		return nil
	}
	loader, _ := api.ctx.Value(loaderContextKey{}).(*store.DataLoader)
	return loader
}
