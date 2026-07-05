// Package render 负责加载 HTML 模板、注册模板函数,并提供渲染辅助。
package render

import (
	"context"
	"html"
	"html/template"
	"net/http"
	"reflect"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"
	"github.com/youthlin/t/f"
)

// themeHTMLRender 为单次响应创建独立的模板函数闭包，避免请求间共享渲染状态。
type themeHTMLRender struct {
	tmpl    *template.Template
	name    string
	data    any
	runtime *TemplateRuntime
}

var _ ginrender.Render = (*themeHTMLRender)(nil)

// WriteContentType 实现 [ginrender.Render] 接口
func (w *themeHTMLRender) WriteContentType(rw http.ResponseWriter) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
}

// Render 实现 [ginrender.Render] 接口
func (w *themeHTMLRender) Render(wr http.ResponseWriter) error {
	ctx := &RequestContext{
		Context:     dataContext(w.data),
		Data:        w.data,
		Runtime:     w.runtime,
		ThemeLoader: dataValue(w.data, ThemeLoaderDataKey),
		Theme:       dataValue(w.data, ThemeDataKey),
	}
	tpl, err := cloneTemplateForRequest(w.tmpl, ctx)
	if err != nil {
		return err
	}
	ctx.Template = tpl
	return tpl.ExecuteTemplate(wr, w.name, w.data)
}

// ========== ========== ========== ========== ========== ========== ==========

// RequestContext 是一次模板渲染独占的运行时上下文。
// 类型保持为 any，避免 render 包依赖 store/theme 包。
type RequestContext struct {
	// Context 是当前 HTTP 请求的 context.Context，从模板 Data 的 __request_context key 提取。
	// 用于传递 trace ID、超时控制、请求取消信号等；若 Data 中不存在则 fallback 到 context.Background()。
	Context context.Context

	// Template 是已 clone 并绑定了请求级模板函数闭包的模板实例。
	// 模板函数（如 listComments）通过它执行自定义评论模板等子模板。
	Template *template.Template

	// Data 是模板渲染的根数据对象（通常是 gin.H），包含所有模板可访问的字段，
	// 如 SiteName、Post、Comments、CurrentUser、Menus 等。
	Data any

	// Runtime 是模板运行时能力提供者，持有 hooks 执行器、组件提供者、
	// 主题选项读取器、hook 调用器等回调。之前作为独立参数传递，现已合并进 RequestContext。
	Runtime *TemplateRuntime

	// ThemeLoader 是当前请求的 DataLoader 实例（any 避免循环依赖 store 包）。
	// 模板函数中用于按需加载额外数据（如文章详情、分类列表等）。
	ThemeLoader any

	// Theme 是当前激活的主题对象（any 避免循环依赖 theme 包）。
	// 模板函数中用于读取主题相关配置（如主题选项、组件区域等）。
	Theme any

	// WidgetOptions 是当前正在渲染的组件的选项键值对。
	// renderWidgets 遍历组件时设置，widgetOption 模板函数读取；渲染完成后清空为 nil。
	WidgetOptions map[string]string
}

// T 从当前请求上下文获取 translator 并翻译，自动 HTML 转义。
// 用法: ctx.T("msg") 或 ctx.T("msg", args...)
// xgettext 通过 T:1 关键字自动提取翻译字符串。
func (ctx *RequestContext) T(msg string, args ...any) string {
	if ctx != nil {
		if tr, ok := dataValue(ctx.Data, "th").(interface{ T(string, ...any) string }); ok && tr != nil {
			return html.EscapeString(tr.T(msg, args...))
		}
	}
	if len(args) > 0 {
		return html.EscapeString(f.Format(msg, args...))
	}
	return html.EscapeString(msg)
}

// WidgetInfo 是模板渲染阶段需要的组件信息。
// 放在 render 包里，避免 renderWidgets 为了读取 theme.WidgetInfo 而使用反射。
type WidgetInfo struct {
	InstanceID   string            // 组件实例 ID，同一组件重复添加时用于区分实例
	ID           string            // 组件 ID
	Source       string            // builtin / theme / plugin
	PluginID     string            // Source=plugin 时的插件 ID
	TemplateName string            // 模板 define 名称，如 "widget_search"
	Options      map[string]string // 组件选项
}

const (
	// ThemeLoaderDataKey 是模板数据中保存当前请求 DataLoader 的内部 key。
	// 它不供模板直接使用，仅用于主题模板函数读取请求级数据。
	ThemeLoaderDataKey = "__theme_loader"
	// ThemeDataKey 是模板数据中保存当前请求主题对象的内部 key。
	// 它不供模板直接使用，仅用于模板函数复用请求级当前主题，避免重复查询 Setting。
	ThemeDataKey = "__theme"
	// ContextDataKey 是模板数据中保存当前请求 context 的内部 key。
	// 前台页面的 Data[ContextDataKey] = c.Request.Context()
	ContextDataKey = "__request_context"
)

const (
	// tplFuncHookInvoke 调用插件通过 RegisterFunc 注册的导出函数（大写开头）。
	// 用法: {{hook_invoke "FuncName" "arg1" value1 "arg2" value2 ...}}
	tplFuncHookInvoke = "hook_invoke"

	// tplFuncThemeOption 读取当前主题的选项值。
	// 用法: {{theme_option "option_id"}}
	tplFuncThemeOption = "theme_option"

	// tplFuncWidgetOption 读取当前渲染组件的选项值。
	// 用法: {{widget_option "key"}}
	tplFuncWidgetOption = "widget_option"

	// tplFuncRenderWidgets 渲染指定区域的组件。
	// 用法: {{render_widgets "area_name" .}}
	tplFuncRenderWidgets = "render_widgets"

	// tplFuncRenderMenu 渲染指定位置的导航菜单。
	// 用法: {{render_menu "location"}} 或 {{render_menu "location" .}}
	tplFuncRenderMenu = "render_menu"

	// tplFuncSlot 渲染指定名称的插槽（hook 注入点）。
	// 用法: {{slot "slot_name" .}}
	tplFuncSlot = "slot"

	// tplFuncPostTitle 输出文章标题（含草稿标记），应用 post.title filter。
	// 用法: {{post_title .Post}}
	tplFuncPostTitle = "post_title"

	// tplFuncPostExcerpt 输出文章摘要，应用 post.excerpt_html filter。
	// 用法: {{post_excerpt .Post}}
	tplFuncPostExcerpt = "post_excerpt"

	// tplFuncPostContent 输出文章正文，应用 post.content_html filter。
	// 用法: {{post_content .Post}}
	tplFuncPostContent = "post_content"

	// tplFuncPostTags 输出文章标签链接列表。
	// 用法: {{post_tags .Post}}
	tplFuncPostTags = "post_tags"

	// tplFuncPostNavigation 输出上一篇/下一篇文章导航链接。
	// 用法: {{post_navigation .}} 或 {{post_navigation . "custom-class"}}
	tplFuncPostNavigation = "post_navigation"

	// tplFuncBodyClass 生成 <body> 标签的 CSS class 属性值。
	// 用法: <body class="{{body_class .}}">
	tplFuncBodyClass = "body_class"

	// tplFuncPostClass 生成文章容器的 CSS class 属性值。
	// 用法: <article class="{{post_class .Post "extra-class"}}">
	tplFuncPostClass = "post_class"

	// tplFuncCommentClass 生成评论 <li> 的 CSS class 属性值。
	// 用法: <li class="{{comment_class . "extra-class"}}">
	tplFuncCommentClass = "comment_class"

	// tplFuncCommentContent 输出评论正文，应用 comment.render_html filter。
	// 用法: {{comment_content .}}（用于自定义评论模板 comment_item）
	tplFuncCommentContent = "comment_content"

	// tplFuncHeadMeta 生成 OpenGraph / Twitter Card meta 标签，应用 head.meta filter。
	// 用法: {{head_meta .}}
	tplFuncHeadMeta = "head_meta"

	// tplFuncListComments 渲染评论列表（仅顶层评论，不含分页）。
	// 用法: {{list_comments .}} 或 {{list_comments . "ol" 44 "my_comment"}}
	tplFuncListComments = "list_comments"

	// tplFuncCommentForm 渲染评论表单（含登录提示）或"评论已关闭"提示。
	// 用法: {{comment_form .}}
	tplFuncCommentForm = "comment_form"

	// tplFuncCommentsPagination 渲染评论分页导航。
	// 用法: {{comments_pagination .}} 或 {{comments_pagination . 2}}
	tplFuncCommentsPagination = "comments_pagination"

	// tplFuncPostsPagination 渲染文章列表分页导航。
	// 用法: {{posts_pagination .}} 或 {{posts_pagination . 2}}
	tplFuncPostsPagination = "posts_pagination"
)

// dataContext 从前台页面的 Data 中通过 [ContextDataKey] 拿到 [context.Context]
func dataContext(data any) context.Context {
	if ctx, ok := dataValue(data, ContextDataKey).(context.Context); ok && ctx != nil {
		return ctx
	}
	return context.Background()
}

func dataValue(data any, key string) any {
	if data == nil {
		return nil
	}
	if m, ok := data.(gin.H); ok {
		return m[key]
	}
	if m, ok := data.(map[string]any); ok {
		return m[key]
	}
	// gin.H 是 map[string]any 的命名类型，通常会命中上面的断言。
	// 这里保留 map 反射兜底，是为了兼容测试或未来调用方传入自定义命名 map 类型；
	// 只读取 string key，不做结构体字段探测，避免模板数据访问规则继续扩散。
	rv := reflect.ValueOf(data)
	if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		v := rv.MapIndex(reflect.ValueOf(key))
		if v.IsValid() && v.CanInterface() {
			return v.Interface()
		}
	}
	return nil
}

func cloneTemplateForRequest(tpl *template.Template, ctx *RequestContext) (*template.Template, error) {
	if tpl == nil {
		return nil, nil
	}
	cloned, err := tpl.Clone()
	if err != nil {
		return nil, err
	}
	// 添加实际函数的实现
	cloned.Funcs(template.FuncMap{
		tplFuncHookInvoke:         func(name string, args ...any) any { return hookInvoke(ctx, name, args...) },
		tplFuncThemeOption:        func(optionID string) string { return themeOption(ctx, optionID) },
		tplFuncWidgetOption:       func(key string) string { return widgetOption(ctx, key) },
		tplFuncRenderWidgets:      func(area string, data any) template.HTML { return renderWidgets(ctx, area, data) },
		tplFuncRenderMenu:         func(location string, data ...any) template.HTML { return renderMenu(ctx, location, data...) },
		tplFuncSlot:               func(name string, data any) template.HTML { return slot(ctx, name, data) },
		tplFuncPostTitle:          func(post any) template.HTML { return postTitle(ctx, post) },
		tplFuncPostExcerpt:        func(post any) template.HTML { return postExcerpt(ctx, post) },
		tplFuncPostContent:        func(post any) template.HTML { return postContent(ctx, post) },
		tplFuncPostTags:           func(post any) template.HTML { return postTags(post) },
		tplFuncPostNavigation:     func(data any, classes ...string) template.HTML { return postNavigation(ctx, data, classes...) },
		tplFuncBodyClass:          func(data any) string { return bodyClass(data) },
		tplFuncPostClass:          func(post any, extra ...string) string { return postClass(post, extra...) },
		tplFuncCommentClass:       func(comment any, extra ...string) string { return commentClass(comment, extra...) },
		tplFuncCommentContent:     func(comment any) template.HTML { return commentContent(ctx, comment) },
		tplFuncHeadMeta:           func(data any) template.HTML { return headMeta(ctx, data) },
		tplFuncListComments:       func(args ...any) template.HTML { return listComments(ctx, args...) },
		tplFuncCommentForm:        func(data any) template.HTML { return commentForm(ctx, data) },
		tplFuncCommentsPagination: func(data any, midSize ...int) template.HTML { return commentsPagination(ctx, data, midSize...) },
		tplFuncPostsPagination:    func(data any, midSize ...int) template.HTML { return postsPagination(ctx, data, midSize...) },
	})
	return cloned, nil
}

// markTplFuncMap 模板占位函数
// 扩展函数在每次 Renderer.Render 时会绑定到请求级 RequestContext。
// 这里提供占位函数，仅用于模板解析阶段识别函数名。
func markTplFuncMap() template.FuncMap {
	return template.FuncMap{
		tplFuncHookInvoke:         func(name string, args ...any) any { return nil },
		tplFuncThemeOption:        func(optionID string) string { return "" },
		tplFuncRenderWidgets:      func(area string, data any) template.HTML { return "" },
		tplFuncRenderMenu:         func(location string, data ...any) template.HTML { return "" },
		tplFuncWidgetOption:       func(key string) string { return "" },
		tplFuncSlot:               func(name string, data any) template.HTML { return "" },
		tplFuncPostTitle:          func(data any) template.HTML { return "" },
		tplFuncPostExcerpt:        func(post any) template.HTML { return "" },
		tplFuncPostContent:        func(post any) template.HTML { return "" },
		tplFuncPostTags:           func(post any) template.HTML { return "" },
		tplFuncPostNavigation:     func(data any, classes ...string) template.HTML { return "" },
		tplFuncBodyClass:          func(data any) string { return "" },
		tplFuncPostClass:          func(post any, extra ...string) string { return "" },
		tplFuncCommentClass:       func(comment any, extra ...string) string { return "" },
		tplFuncCommentContent:     func(comment any) template.HTML { return "" },
		tplFuncHeadMeta:           func(data any) template.HTML { return "" },
		tplFuncListComments:       func(args ...any) template.HTML { return "" },
		tplFuncCommentForm:        func(data any) template.HTML { return "" },
		tplFuncCommentsPagination: func(data any, midSize ...int) template.HTML { return "" },
		tplFuncPostsPagination:    func(data any, midSize ...int) template.HTML { return "" },
	}
}
