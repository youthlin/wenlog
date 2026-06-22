// Package render 负责加载 HTML 模板、注册模板函数,并提供渲染辅助。
package render

import (
	"html/template"
	"net/http"
	"reflect"

	ginrender "github.com/gin-gonic/gin/render"
)

// widgetHTMLRender 为单次响应创建独立的模板函数闭包，避免请求间共享渲染状态。
type widgetHTMLRender struct {
	tmpl    *template.Template
	name    string
	data    any
	runtime *TemplateRuntime
}

var _ ginrender.Render = (*widgetHTMLRender)(nil)

// RequestContext 是一次模板渲染独占的运行时上下文。
// 类型保持为 any，避免 render 包依赖 store/theme 包。
type RequestContext struct {
	Template      *template.Template
	ThemeLoader   any
	Theme         any
	WidgetOptions map[string]string
	values        map[any]any
}

const (
	// ThemeLoaderDataKey 是模板数据中保存当前请求 DataLoader 的内部 key。
	// 它不供模板直接使用，仅用于 themeData provider 读取请求级数据。
	ThemeLoaderDataKey = "__theme_loader"
	// ThemeDataKey 是模板数据中保存当前请求主题对象的内部 key。
	// 它不供模板直接使用，仅用于模板函数复用请求级当前主题，避免重复查询 Setting。
	ThemeDataKey = "__theme"
)

// Render 实现 [ginrender.Render] 接口
func (w *widgetHTMLRender) Render(wr http.ResponseWriter) error {
	ctx := &RequestContext{
		ThemeLoader: dataValue(w.data, ThemeLoaderDataKey),
		Theme:       dataValue(w.data, ThemeDataKey),
		values:      make(map[any]any),
	}
	tpl, err := cloneTemplateForRequest(w.tmpl, ctx, w.runtime)
	if err != nil {
		return err
	}
	ctx.Template = tpl
	return tpl.ExecuteTemplate(wr, w.name, w.data)
}

func dataValue(data any, key string) any {
	if data == nil {
		return nil
	}
	if m, ok := data.(map[string]any); ok {
		return m[key]
	}
	rv := reflect.ValueOf(data)
	if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		v := rv.MapIndex(reflect.ValueOf(key))
		if v.IsValid() && v.CanInterface() {
			return v.Interface()
		}
	}
	return nil
}

// WriteContentType 实现 [ginrender.Render] 接口
func (w *widgetHTMLRender) WriteContentType(rw http.ResponseWriter) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
}

// Value 返回当前请求上下文中的私有缓存值。
func (ctx *RequestContext) Value(key any) any {
	if ctx == nil || ctx.values == nil {
		return nil
	}
	return ctx.values[key]
}

// SetValue 设置当前请求上下文中的私有缓存值。
func (ctx *RequestContext) SetValue(key, value any) {
	if ctx == nil {
		return
	}
	if ctx.values == nil {
		ctx.values = make(map[any]any)
	}
	ctx.values[key] = value
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
		"themeData":     func(name string, args ...any) any { return themeData(runtime, ctx, name, args...) },
		"themeOption":   func(optionID string) string { return themeOption(runtime, ctx, optionID) },
		"themeWidgets":  func(area string) any { return themeWidgets(runtime, ctx, area) },
		"renderWidgets": func(area string, data any) template.HTML { return renderWidgets(runtime, ctx, area, data) },
		"widgetOption":  func(key string) string { return widgetOption(ctx, key) },
	})
	return cloned, nil
}
