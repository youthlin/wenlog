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
		ThemeLoader: dataValue(w.data, ThemeLoaderDataKey),
		Theme:       dataValue(w.data, ThemeDataKey),
	}
	tpl, err := cloneTemplateForRequest(w.tmpl, ctx, w.runtime)
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
	Context       context.Context
	Template      *template.Template
	Data          any
	ThemeLoader   any
	Theme         any
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
	// hookInvoke: 调用 functions.go 中通过 [RegisterFunc] 注册的函数
	// {{hookInvoke "<funcName>" "<argName>" argValue ["<argName2>" argValue2]}}
	tplFuncHookInvoke         = "hook_invoke"
	tplFuncOption             = "option"
	tplFuncThemeOption        = "theme_option"
	tplFuncWidgetOption       = "widget_option"
	tplFuncRenderWidgets      = "render_widgets"
	tplFuncWidgets            = "widgets"
	tplFuncRenderMenu         = "render_menu"
	tplFuncSlot               = "slot"
	tplFuncPostTitle          = "post_title"
	tplFuncPostExcerpt        = "post_excerpt"
	tplFuncPostContent        = "post_content"
	tplFuncPostTags           = "post_tags"
	tplFuncPostNavigation     = "post_navigation"
	tplFuncBodyClass          = "body_class"
	tplFuncPostClass          = "post_class"
	tplFuncCommentClass       = "comment_class"
	tplFuncCommentContent     = "comment_content"
	tplFuncHeadMeta           = "head_meta"
	tplFuncListComments       = "list_comments"
	tplFuncCommentForm        = "comment_form"
	tplFuncCommentsPagination = "comments_pagination"
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

func cloneTemplateForRequest(tpl *template.Template, ctx *RequestContext, runtime *TemplateRuntime) (*template.Template, error) {
	if tpl == nil {
		return nil, nil
	}
	cloned, err := tpl.Clone()
	if err != nil {
		return nil, err
	}
	// 添加实际函数的实现
	cloned.Funcs(template.FuncMap{
		tplFuncHookInvoke:     func(name string, args ...any) any { return hookInvoke(runtime, ctx, name, args...) },
		tplFuncThemeOption:    func(optionID string) string { return themeOption(runtime, ctx, optionID) },
		tplFuncWidgetOption:   func(key string) string { return widgetOption(ctx, key) },
		tplFuncRenderWidgets:  func(area string, data any) template.HTML { return renderWidgets(runtime, ctx, area, data) },
		tplFuncWidgets:        func(area string, data any) template.HTML { return renderWidgets(runtime, ctx, area, data) },
		tplFuncRenderMenu:     func(location string, data ...any) template.HTML { return renderMenu(ctx, location, data...) },
		tplFuncSlot:           func(name string, data any) template.HTML { return slot(runtime, ctx, name, data) },
		tplFuncPostTitle:      func(post any) template.HTML { return postTitle(runtime, ctx, post) },
		tplFuncPostExcerpt:    func(post any) template.HTML { return postExcerpt(runtime, ctx, post) },
		tplFuncPostContent:    func(post any) template.HTML { return postContent(runtime, ctx, post) },
		tplFuncPostTags:       func(post any) template.HTML { return postTags(post) },
		tplFuncPostNavigation: func(data any, classes ...string) template.HTML { return postNavigation(ctx, data, classes...) },
		tplFuncBodyClass:      func(data any) string { return bodyClass(data) },
		tplFuncPostClass:      func(post any, extra ...string) string { return postClass(post, extra...) },
		tplFuncCommentClass:   func(comment any, extra ...string) string { return commentClass(comment, extra...) },
		tplFuncCommentContent: func(comment any) template.HTML { return commentContent(runtime, ctx, comment) },
		tplFuncHeadMeta:       func(data any) template.HTML { return headMeta(runtime, ctx, data) },
		tplFuncListComments:       func(args ...any) template.HTML { return listComments(runtime, ctx, args...) },
		tplFuncCommentForm:        func(data any) template.HTML { return commentForm(runtime, ctx, data) },
		tplFuncCommentsPagination: func(data any) template.HTML { return commentsPagination(ctx, data) },
	})
	return cloned, nil
}

// markTplFuncMap 模板占位函数
// 扩展函数在每次 Renderer.Render 时会绑定到请求级 RequestContext。
// 这里提供占位函数，仅用于模板解析阶段识别函数名。
func markTplFuncMap() template.FuncMap {
	return template.FuncMap{
		tplFuncHookInvoke:     func(name string, args ...any) any { return nil },
		tplFuncThemeOption:    func(optionID string) string { return "" },
		tplFuncOption:         func(optionID string) string { return "" },
		tplFuncRenderWidgets:  func(area string, data any) template.HTML { return "" },
		tplFuncWidgets:        func(area string, data any) template.HTML { return "" },
		tplFuncRenderMenu:     func(location string, data ...any) template.HTML { return "" },
		tplFuncWidgetOption:   func(key string) string { return "" },
		tplFuncSlot:           func(name string, data any) template.HTML { return "" },
		tplFuncPostTitle:      func(data any) template.HTML { return "" },
		tplFuncPostExcerpt:    func(post any) template.HTML { return "" },
		tplFuncPostContent:    func(post any) template.HTML { return "" },
		tplFuncPostTags:       func(post any) template.HTML { return "" },
		tplFuncPostNavigation: func(data any, classes ...string) template.HTML { return "" },
		tplFuncBodyClass:      func(data any) string { return "" },
		tplFuncPostClass:      func(post any, extra ...string) string { return "" },
		tplFuncCommentClass:   func(comment any, extra ...string) string { return "" },
		tplFuncCommentContent: func(comment any) template.HTML { return "" },
		tplFuncHeadMeta:       func(data any) template.HTML { return "" },
		tplFuncListComments:   func(args ...any) template.HTML { return "" },
		tplFuncCommentForm:    func(data any) template.HTML { return "" },
		tplFuncCommentsPagination: func(data any) template.HTML { return "" },
	}
}
