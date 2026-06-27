package render

import (
	"context"
	"html"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"

	ginrender "github.com/gin-gonic/gin/render"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/wxr"
	"github.com/youthlin/blog/themeapi"
	"github.com/youthlin/blog/web"
)

// LoadThemeTemplates 加载主题模板目录。先解析主题模板，再补充内置组件模板，
// 再补充主题自定义组件模板（覆盖内置），最后补充 admin/auth 模板中缺失的。
func (r *Renderer) LoadThemeTemplates(themeDir string) error {
	if r == nil {
		return nil
	}
	themeFS := os.DirFS(themeDir)
	// 解析主题模板（主题必须自包含，不再从默认主题 fallback）
	themeTpl, err := parseTemplates(themeFS, r.pattern)
	if err != nil {
		return err
	}
	// 补充主题自定义组件模板（themes/<name>/widgets/），优先于内置
	widgetsDir := filepath.Join(filepath.Dir(themeDir), "widgets")
	if _, err := os.Stat(widgetsDir); err == nil {
		widgetsFS := os.DirFS(widgetsDir)
		r.fallbackWidgets(themeTpl, widgetsFS)
	}
	// 补充内置组件模板（web/widgets/），仅补充主题未提供的
	r.fallbackWidgets(themeTpl, nil)
	// 补充 admin/auth 模板中缺失的（基础设施，非主题范畴）
	r.fallbackFromDefaultFS(themeTpl)
	r.mu.Lock()
	r.tpl = themeTpl
	r.fsys = themeFS
	r.hot = false
	r.themeDir = themeDir
	r.mu.Unlock()
	return nil
}

// fallbackWidgets 补充组件模板。如果 widgetsFS 为 nil，从 embed web.Widgets 加载内置组件；
// 否则从指定 FS 加载（主题自定义组件）。仅补充 themeTpl 中不存在的模板。
func (r *Renderer) fallbackWidgets(themeTpl *template.Template, widgetsFS fs.FS) {
	if widgetsFS == nil {
		// 从 embed 加载内置组件（web.Widgets embed 根是 widgets/ 子目录）
		var err error
		widgetsFS, err = fs.Sub(web.Widgets, "widgets")
		if err != nil {
			return
		}
	}
	if !hasMatchingFiles(widgetsFS, r.pattern) {
		return
	}
	widgetsTpl, err := parseTemplates(widgetsFS, r.pattern)
	if err != nil {
		return
	}
	for _, t := range widgetsTpl.Templates() {
		// ParseFS 会为每个文件额外生成一个以文件名命名的模板。
		// 组件文件统一命名为 widget_<id>.gohtml，因此这里只补充真正的组件定义，
		// 避免文件名模板遮蔽前台页面的 fallback 链。
		if !strings.HasPrefix(t.Name(), "widget_") {
			continue
		}
		if themeTpl.Lookup(t.Name()) == nil {
			_, _ = themeTpl.AddParseTree(t.Name(), t.Tree)
		}
	}
}

// hasMatchingFiles 检查 fsys 中是否存在匹配 pattern 的文件。
func hasMatchingFiles(fsys fs.FS, pattern string) bool {
	matches, err := fs.Glob(fsys, pattern)
	return err == nil && len(matches) > 0
}

// fallbackFromDefaultFS 从 admin/auth 模板补充缺失的模板。
func (r *Renderer) fallbackFromDefaultFS(themeTpl *template.Template) {
	if r.defaultFS == nil || !hasMatchingFiles(r.defaultFS, r.pattern) {
		return
	}
	defaultTpl, err := parseTemplates(r.defaultFS, r.pattern)
	if err != nil {
		return
	}
	for _, t := range defaultTpl.Templates() {
		if themeTpl.Lookup(t.Name()) == nil {
			_, _ = themeTpl.AddParseTree(t.Name(), t.Tree)
		}
	}
}

// ResetToDefault 重置为默认主题模板 + admin/auth 回退。
// 优先从磁盘 themes/default/templates/ 加载，不存在时回退 embed。
func (r *Renderer) ResetToDefault() error {
	if r == nil {
		return nil
	}
	// 先尝试磁盘默认主题
	themeDir := filepath.Join("themes", "default", "templates")
	if _, err := os.Stat(themeDir); err == nil {
		return r.LoadThemeTemplates(themeDir)
	}
	// 磁盘不存在时从 embed 加载默认主题
	if r.defaultThemeFS != nil {
		return r.loadThemeTemplatesFS(r.defaultThemeFS)
	}
	// 兜底：只用 admin/auth 模板
	if r.defaultFS == nil {
		return nil
	}
	tpl, err := parseTemplates(r.defaultFS, r.pattern)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.tpl = tpl
	r.fsys = r.defaultFS
	r.hot = r.defaultHot
	r.themeDir = ""
	r.mu.Unlock()
	return nil
}

// loadThemeTemplatesFS 从 fs.FS 加载主题模板（如 embed 默认主题），再补充 admin/auth 回退。
func (r *Renderer) loadThemeTemplatesFS(themeFS fs.FS) error {
	themeTpl, err := parseTemplates(themeFS, r.pattern)
	if err != nil {
		return err
	}
	// 补充内置组件模板
	r.fallbackWidgets(themeTpl, nil)
	// 补充 admin/auth 模板
	if r.defaultFS != nil && hasMatchingFiles(r.defaultFS, r.pattern) {
		defaultTpl, err := parseTemplates(r.defaultFS, r.pattern)
		if err != nil {
			return err
		}
		for _, t := range defaultTpl.Templates() {
			if themeTpl.Lookup(t.Name()) == nil {
				if _, err := themeTpl.AddParseTree(t.Name(), t.Tree); err != nil {
					return err
				}
			}
		}
	}
	r.mu.Lock()
	r.tpl = themeTpl
	r.fsys = themeFS
	r.hot = false
	r.themeDir = ""
	r.mu.Unlock()
	return nil
}

// LoadPreviewTheme 加载预览主题的模板到独立缓存，不影响主模板。
func (r *Renderer) LoadPreviewTheme(themeDir, themeName string) error {
	if r == nil {
		return nil
	}
	themeFS := os.DirFS(themeDir)
	themeTpl, err := parseTemplates(themeFS, r.pattern)
	if err != nil {
		return err
	}
	// 补充主题自定义组件模板
	widgetsDir := filepath.Join(filepath.Dir(themeDir), "widgets")
	if _, err := os.Stat(widgetsDir); err == nil {
		widgetsFS := os.DirFS(widgetsDir)
		r.fallbackWidgets(themeTpl, widgetsFS)
	}
	// 补充内置组件模板
	r.fallbackWidgets(themeTpl, nil)
	r.fallbackFromDefaultFS(themeTpl)
	r.mu.Lock()
	r.previewTpl = themeTpl
	r.previewThemeName = themeName
	r.mu.Unlock()
	return nil
}

// ClearPreviewTheme 清除预览主题模板缓存。
func (r *Renderer) ClearPreviewTheme() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.previewTpl = nil
	r.previewThemeName = ""
	r.mu.Unlock()
}

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
		return &widgetHTMLRender{tmpl: previewTpl, name: name, data: data, runtime: &r.themeRuntime}
	}
	return &widgetHTMLRender{tmpl: mainTpl, name: name, data: data, runtime: &r.themeRuntime}
}

// TemplateRuntime 保存主题模板函数需要调用的运行时能力。
// 它归属于 Renderer 实例，避免不同渲染器之间通过包级全局变量串联。
type TemplateRuntime struct {
	optionProvider       atomic.Value // stores optionProviderHolder
	themeInvokeProvider  atomic.Value // stores themeInvokeProviderHolder
	themeWidgetsProvider atomic.Value // stores themeWidgetsProviderHolder
	hookProvider         atomic.Value // stores hookProviderHolder
	pluginWidgetProvider atomic.Value // stores pluginWidgetProviderHolder
}

type hookProvider interface {
	DoAction(ctx context.Context, name string, args ...any)
	ApplyFilters(ctx context.Context, name string, value any, args ...any) any
}

type optionProviderHolder struct {
	fn func(ctx *RequestContext, optionID string) string
}

type themeInvokeProviderHolder struct {
	fn func(ctx *RequestContext, name string, args ...any) any
}

type themeWidgetsProviderHolder struct {
	fn func(ctx *RequestContext, area string) any
}

type hookProviderHolder struct {
	provider hookProvider
}

type pluginWidgetProviderHolder struct {
	fn func(ctx *RequestContext, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool)
}

// SetOptionProvider 设置 option 读取函数。
func (r *Renderer) SetOptionProvider(fn func(ctx *RequestContext, optionID string) string) {
	if r == nil {
		return
	}
	r.themeRuntime.optionProvider.Store(optionProviderHolder{fn: fn})
}

// SetThemeInvokeProvider 设置 themeInvoke 模板函数的实现（由 theme.Manager 注入）。
func (r *Renderer) SetThemeInvokeProvider(fn func(ctx *RequestContext, name string, args ...any) any) {
	if r == nil {
		return
	}
	r.themeRuntime.themeInvokeProvider.Store(themeInvokeProviderHolder{fn: fn})
}

// SetThemeWidgetsProvider 设置 themeWidgets 模板函数的实现。
func (r *Renderer) SetThemeWidgetsProvider(fn func(ctx *RequestContext, area string) any) {
	if r == nil {
		return
	}
	r.themeRuntime.themeWidgetsProvider.Store(themeWidgetsProviderHolder{fn: fn})
}

// SetHookProvider 设置插件 Hook Registry，用于 pluginSlot/postContent/commentContent 等模板函数。
func (r *Renderer) SetHookProvider(provider hookProvider) {
	if r == nil {
		return
	}
	r.themeRuntime.hookProvider.Store(hookProviderHolder{provider: provider})
}

// SetPluginWidgetProvider 设置插件组件渲染函数。
func (r *Renderer) SetPluginWidgetProvider(fn func(ctx *RequestContext, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool)) {
	if r == nil {
		return
	}
	r.themeRuntime.pluginWidgetProvider.Store(pluginWidgetProviderHolder{fn: fn})
}

func themeInvoke(runtime *TemplateRuntime, ctx *RequestContext, name string, args ...any) any {
	if runtime == nil {
		return nil
	}
	provider, _ := runtime.themeInvokeProvider.Load().(themeInvokeProviderHolder)
	if provider.fn == nil {
		return nil
	}
	return provider.fn(ctx, name, args...)
}

func themeOption(runtime *TemplateRuntime, ctx *RequestContext, optionID string) string {
	if runtime == nil {
		return ""
	}
	provider, _ := runtime.optionProvider.Load().(optionProviderHolder)
	if provider.fn == nil {
		return ""
	}
	return provider.fn(ctx, optionID)
}

func themeWidgets(runtime *TemplateRuntime, ctx *RequestContext, area string) any {
	if runtime == nil {
		return nil
	}
	provider, _ := runtime.themeWidgetsProvider.Load().(themeWidgetsProviderHolder)
	if provider.fn == nil {
		return nil
	}
	return provider.fn(ctx, area)
}

func hooks(runtime *TemplateRuntime) hookProvider {
	if runtime == nil {
		return nil
	}
	provider, _ := runtime.hookProvider.Load().(hookProviderHolder)
	return provider.provider
}

func pluginWidgetRenderer(runtime *TemplateRuntime) func(ctx *RequestContext, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool) {
	if runtime == nil {
		return nil
	}
	provider, _ := runtime.pluginWidgetProvider.Load().(pluginWidgetProviderHolder)
	return provider.fn
}

func requestContext(ctx *RequestContext) context.Context {
	var req context.Context
	if ctx != nil && ctx.Context != nil {
		req = ctx.Context
	} else {
		req = context.Background()
	}
	if ctx != nil && ctx.ThemeLoader != nil {
		req = themeapi.WithDataLoader(req, ctx.ThemeLoader)
	}
	return req
}

// pluginSlot 触发一个模板 slot action，并收集插件写入的 HTML。
func pluginSlot(runtime *TemplateRuntime, ctx *RequestContext, name string, data any) template.HTML {
	h := hooks(runtime)
	if h == nil || name == "" {
		return ""
	}
	var result strings.Builder
	h.DoAction(requestContext(ctx), name, &result, data)
	return template.HTML(result.String())
}

// postContent 输出文章正文，并应用 post.content_html filter。
func postContent(runtime *TemplateRuntime, ctx *RequestContext, post any) template.HTML {
	html := postContentHTML(post)
	if h := hooks(runtime); h != nil {
		html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), "post.content_html", string(html), post))
	}
	return html
}

func postContentHTML(post any) template.HTML {
	switch p := post.(type) {
	case *model.Post:
		if p == nil {
			return ""
		}
		return detailHTML(p)
	case model.Post:
		return detailHTML(&p)
	default:
		content := reflectStringField(post, "Content")
		id := reflectUintField(post, "ID")
		if content == "" {
			return ""
		}
		return template.HTML(addSrcSet(HighlightCodeBlocks(SanitizeHTML(wxr.RenderDetail(content, id)))))
	}
}

// commentContent 输出评论正文，并应用 comment.render_html filter。
func commentContent(runtime *TemplateRuntime, ctx *RequestContext, comment any) template.HTML {
	html := template.HTML(html.EscapeString(reflectStringField(comment, "Content")))
	if h := hooks(runtime); h != nil {
		html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), "comment.render_html", string(html), comment))
	}
	return html
}

func htmlFromFilterValue(v any) template.HTML {
	switch val := v.(type) {
	case template.HTML:
		return val
	case string:
		return template.HTML(val)
	case []byte:
		return template.HTML(string(val))
	default:
		return ""
	}
}

func reflectStringField(v any, name string) string {
	rv := indirectValue(v)
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return ""
	}
	f := rv.FieldByName(name)
	if f.IsValid() && f.Kind() == reflect.String {
		return f.String()
	}
	return ""
}

func reflectUintField(v any, name string) uint {
	rv := indirectValue(v)
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return 0
	}
	f := rv.FieldByName(name)
	if !f.IsValid() {
		return 0
	}
	switch f.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return uint(f.Uint())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if f.Int() > 0 {
			return uint(f.Int())
		}
	}
	return 0
}

func indirectValue(v any) reflect.Value {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) {
		if rv.IsNil() {
			return reflect.Value{}
		}
		rv = rv.Elem()
	}
	return rv
}

// renderWidgets 渲染指定区域的所有组件，返回 HTML。
func renderWidgets(runtime *TemplateRuntime, ctx *RequestContext, area string, data any) template.HTML {
	if runtime == nil || ctx == nil || ctx.Template == nil {
		return ""
	}
	provider, _ := runtime.themeWidgetsProvider.Load().(themeWidgetsProviderHolder)
	if provider.fn == nil {
		return ""
	}
	widgets := provider.fn(ctx, area)
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
		for item.IsValid() && (item.Kind() == reflect.Interface || item.Kind() == reflect.Pointer) {
			if item.IsNil() {
				item = reflect.Value{}
				break
			}
			item = item.Elem()
		}
		if item.IsValid() && item.Kind() == reflect.Struct {
			tmplName := item.FieldByName("TemplateName")
			widgetID := reflectStringValue(item.FieldByName("ID"))
			source := reflectStringValue(item.FieldByName("Source"))
			pluginID := reflectStringValue(item.FieldByName("PluginID"))
			// 设置当前组件选项
			optsField := item.FieldByName("Options")
			if optsField.IsValid() && optsField.Kind() == reflect.Map && !optsField.IsNil() {
				ctx.WidgetOptions = optsField.Interface().(map[string]string)
			} else {
				ctx.WidgetOptions = nil
			}
			html, ok := renderWidgetItem(runtime, ctx, source, pluginID, widgetID, tmplName, data)
			if !ok {
				continue
			}
			if h := hooks(runtime); h != nil {
				var widget any
				if item.CanInterface() {
					widget = item.Interface()
				}
				html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), "widget.render_html", string(html), widget, area, data))
			}
			result.WriteString(string(html))
		}
	}
	ctx.WidgetOptions = nil
	return template.HTML(result.String())
}

func renderWidgetItem(runtime *TemplateRuntime, ctx *RequestContext, source, pluginID, widgetID string, tmplName reflect.Value, data any) (template.HTML, bool) {
	if source == "plugin" {
		if renderer := pluginWidgetRenderer(runtime); renderer != nil {
			return renderer(ctx, pluginID, widgetID, ctx.WidgetOptions, data)
		}
		return "", false
	}
	if !tmplName.IsValid() || tmplName.Kind() != reflect.String || tmplName.String() == "" {
		return "", false
	}
	var itemHTML strings.Builder
	if err := ctx.Template.ExecuteTemplate(&itemHTML, tmplName.String(), data); err != nil {
		return "", false
	}
	return template.HTML(itemHTML.String()), true
}

// widgetOption 模板函数：读取当前渲染组件的选项值。
func widgetOption(ctx *RequestContext, key string) string {
	if ctx == nil || ctx.WidgetOptions == nil {
		return ""
	}
	return ctx.WidgetOptions[key]
}

func reflectStringValue(v reflect.Value) string {
	if v.IsValid() && v.Kind() == reflect.String {
		return v.String()
	}
	return ""
}
