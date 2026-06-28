// Package render 负责加载 HTML 模板、注册模板函数,并提供渲染辅助。
package render

import (
	"context"
	"html/template"
	"net/http"
	"reflect"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"
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
	tplFuncHookInvoke     = "hookInvoke"
	tplFuncThemeData      = "themeData"
	tplFuncThemeOption    = "themeOption"
	tplFuncOption         = "option"
	tplFuncRenderWidgets  = "renderWidgets"
	tplFuncWidgets        = "widgets"
	tplFuncRenderMenu     = "renderMenu"
	tplFuncWidgetOption   = "widgetOption"
	tplFuncSlot           = "slot"
	tplFuncPostTitle      = "postTitle"
	tplFuncPostExcerpt    = "postExcerpt"
	tplFuncPostContent    = "postContent"
	tplFuncPostTags       = "postTags"
	tplFuncPostNavigation = "postNavigation"
	tplFuncBodyClass      = "bodyClass"
	tplFuncPostClass      = "postClass"
	tplFuncCommentClass   = "commentClass"
	tplFuncCommentContent = "commentContent"
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
	cloned.Funcs(template.FuncMap{
		tplFuncHookInvoke:     func(name string, args ...any) any { return hookInvoke(runtime, ctx, name, args...) },
		tplFuncThemeData:      func(name string, args ...any) any { return hookInvoke(runtime, ctx, name, args...) },
		tplFuncThemeOption:    func(optionID string) string { return themeOption(runtime, ctx, optionID) },
		tplFuncOption:         func(optionID string) string { return themeOption(runtime, ctx, optionID) },
		tplFuncRenderWidgets:  func(area string, data any) template.HTML { return renderWidgets(runtime, ctx, area, data) },
		tplFuncWidgets:        func(area string, data any) template.HTML { return renderWidgets(runtime, ctx, area, data) },
		tplFuncRenderMenu:     func(location string, data ...any) template.HTML { return renderMenu(ctx, location, data...) },
		tplFuncWidgetOption:   func(key string) string { return widgetOption(ctx, key) },
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
	})
	return cloned, nil
}
