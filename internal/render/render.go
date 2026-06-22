// Package render 负责加载 HTML 模板、注册模板函数,并提供渲染辅助。
package render

import (
	"html/template"
	"net/http"
	"reflect"
	"sync"

	ginrender "github.com/gin-gonic/gin/render"
)

// widgetHTMLRender 在渲染前设置 currentTemplate，使 renderWidgets 能访问模板实例。
type widgetHTMLRender struct {
	tmpl *template.Template
	name string
	data any
}

var _ ginrender.Render = (*widgetHTMLRender)(nil)

var (
	// renderStateMu 串行化模板执行期间的包级渲染状态。
	// 当前实现的 renderWidgets/widgetOption/themeData 依赖模板执行期状态，必须避免并发请求互相覆盖。
	renderStateMu sync.Mutex
	// currentTemplate 存储当前模板实例，供 renderWidgets 使用。
	currentTemplate *template.Template
	// currentThemeLoader 存储当前模板渲染请求对应的 DataLoader。
	// 类型保持为 any，避免 render 包依赖 store 包。
	currentThemeLoader any
	// currentTheme 存储当前模板渲染请求对应的主题对象。
	// 类型保持为 any，避免 render 包依赖 theme 包。
	currentTheme any
	// currentWidgetOptions 存储当前渲染组件的选项，供 widgetOption 模板函数使用。
	currentWidgetOptions map[string]string
)

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
	renderStateMu.Lock()
	defer renderStateMu.Unlock()

	currentTemplate = w.tmpl
	currentThemeLoader = dataValue(w.data, ThemeLoaderDataKey)
	currentTheme = dataValue(w.data, ThemeDataKey)
	currentWidgetOptions = nil
	defer func() {
		currentTemplate = nil
		currentThemeLoader = nil
		currentTheme = nil
		currentWidgetOptions = nil
	}()

	return w.tmpl.ExecuteTemplate(wr, w.name, w.data)
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

// CurrentThemeLoader 返回当前模板渲染请求对应的 DataLoader。
// 调用方需要在模板执行期间读取；模板执行由 renderStateMu 串行化。
func CurrentThemeLoader() any {
	return currentThemeLoader
}

// CurrentTheme 返回当前模板渲染请求对应的主题对象。
// 调用方需要在模板执行期间读取；模板执行由 renderStateMu 串行化。
func CurrentTheme() any {
	return currentTheme
}
