package render

import (
	"html/template"
	"reflect"
	"strings"

	ginrender "github.com/gin-gonic/gin/render"
)

// TemplateHierarchy 定义页面类型到模板文件名的 fallback 链。
// 主题只需提供链中任一模板即可，系统按顺序查找第一个存在的。
var TemplateHierarchy = map[string][]string{
	"index":   {"index.gohtml"},
	"post":    {"post.gohtml", "index.gohtml"},
	"page":    {"page.gohtml", "post.gohtml", "index.gohtml"},
	"search":  {"search.gohtml", "list.gohtml", "index.gohtml"},
	"list":    {"list.gohtml", "index.gohtml"},
	"archive": {"archive.gohtml", "list.gohtml", "index.gohtml"},
	"error":   {"error.gohtml"},
}

// ResolveTemplate 根据页面类型查找主题中第一个存在的模板。
// 如果链中所有模板都不存在，返回链中第一个（让 Go template 报错）。
func (r *Renderer) ResolveTemplate(pageType string) string {
	r.mu.RLock()
	tpl := r.tpl
	r.mu.RUnlock()
	return resolveTemplateIn(tpl, pageType)
}

// ResolveTemplateWithPreview 根据预览主题状态在对应模板集中解析页面类型。
func (r *Renderer) ResolveTemplateWithPreview(pageType, previewName string) string {
	r.mu.RLock()
	tpl := r.tpl
	if previewName != "" && r.previewTpl != nil && previewName == r.previewThemeName {
		tpl = r.previewTpl
	}
	r.mu.RUnlock()
	return resolveTemplateIn(tpl, pageType)
}

func resolveTemplateIn(tpl *template.Template, pageType string) string {
	chain, ok := TemplateHierarchy[pageType]
	if !ok {
		return pageType + ".gohtml"
	}
	for _, name := range chain {
		if tpl != nil && tpl.Lookup(name) != nil {
			return name
		}
	}
	return chain[0]
}

// PreviewInstance 返回预览主题的模板实例。previewName 需与 LoadPreviewTheme 时一致。
// 如果预览未激活或名称不匹配，回退到主模板。
func (r *Renderer) PreviewInstance(name string, data any, previewName string) ginrender.Render {
	r.mu.RLock()
	previewTpl := r.previewTpl
	cachedName := r.previewThemeName
	mainTpl := r.tpl
	r.mu.RUnlock()

	if previewName != "" && previewTpl != nil && previewName == cachedName {
		return &widgetHTMLRender{tmpl: previewTpl, name: name, data: data}
	}
	return &widgetHTMLRender{tmpl: mainTpl, name: name, data: data}
}

// optionProvider 是 themeData "option" 调用的实现，由 web.go 注入。
var optionProvider func(optionID string) string

// themeDataProvider 是 themeData 模板函数的实际实现，由 theme.Manager 注入。
var themeDataProvider func(name string, args ...any) any

// themeWidgetsProvider 是 themeWidgets 模板函数的实际实现，由 handler 层注入。
var themeWidgetsProvider func(area string) any

// SetOptionProvider 设置 option 读取函数。
func SetOptionProvider(fn func(optionID string) string) {
	optionProvider = fn
}

// SetThemeDataProvider 设置 themeData 模板函数的实现（由 theme.Manager 注入）。
func SetThemeDataProvider(fn func(name string, args ...any) any) {
	themeDataProvider = fn
}

// SetThemeWidgetsProvider 设置 themeWidgets 模板函数的实现。
func SetThemeWidgetsProvider(fn func(area string) any) {
	themeWidgetsProvider = fn
}

func themeData(name string, args ...any) any {
	// "option" 是特殊 provider：读取主题选项
	if name == "option" && optionProvider != nil {
		if len(args) > 0 {
			if id, ok := args[0].(string); ok {
				return optionProvider(id)
			}
		}
		return ""
	}
	if themeDataProvider == nil {
		return nil
	}
	return themeDataProvider(name, args...)
}

func themeWidgets(area string) any {
	if themeWidgetsProvider == nil {
		return nil
	}
	return themeWidgetsProvider(area)
}

// renderWidgets 渲染指定区域的所有组件，返回 HTML。
func renderWidgets(area string, data any) template.HTML {
	if themeWidgetsProvider == nil || currentTemplate == nil {
		return ""
	}
	widgets := themeWidgetsProvider(area)
	if widgets == nil {
		return ""
	}
	// widgets is []theme.WidgetInfo, use reflection to avoid circular import
	rv := reflect.ValueOf(widgets)
	if rv.Kind() != reflect.Slice {
		return ""
	}
	var result strings.Builder
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if item.Kind() == reflect.Struct {
			tmplName := item.FieldByName("TemplateName")
			if !tmplName.IsValid() {
				continue
			}
			// 设置当前组件选项
			optsField := item.FieldByName("Options")
			if optsField.IsValid() && !optsField.IsNil() {
				currentWidgetOptions = optsField.Interface().(map[string]string)
			} else {
				currentWidgetOptions = nil
			}
			if err := currentTemplate.ExecuteTemplate(&result, tmplName.String(), data); err != nil {
				continue
			}
		}
	}
	currentWidgetOptions = nil
	return template.HTML(result.String())
}

// widgetOption 模板函数：读取当前渲染组件的选项值。
func widgetOption(key string) string {
	if currentWidgetOptions == nil {
		return ""
	}
	return currentWidgetOptions[key]
}
