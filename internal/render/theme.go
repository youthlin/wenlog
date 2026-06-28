package render

import (
	"context"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"

	ginrender "github.com/gin-gonic/gin/render"
	"github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/wxr"
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
	"index":    {"index.gohtml"},
	"post":     {"post.gohtml", "index.gohtml"},
	"page":     {"page.gohtml", "post.gohtml", "index.gohtml"},
	"category": {"category.gohtml", "list.gohtml", "index.gohtml"},
	"tag":      {"tag.gohtml", "list.gohtml", "index.gohtml"},
	"search":   {"search.gohtml", "list.gohtml", "index.gohtml"},
	"list":     {"list.gohtml", "index.gohtml"},
	"archive":  {"archive.gohtml", "list.gohtml", "index.gohtml"},
	"error":    {"error.gohtml"},
}

// ResolveTemplate 根据页面类型查找主题中第一个存在的模板。
// 如果链中所有模板都不存在，返回链中第一个（让 Go template 报错）。
func (r *Renderer) ResolveTemplate(pageType string) string {
	r.mu.RLock()
	tpl := r.tpl
	r.mu.RUnlock()
	return resolveTemplateIn(tpl, pageType, nil)
}

// ResolveTemplateWithPreview 根据预览主题状态在对应模板集中解析页面类型。
func (r *Renderer) ResolveTemplateWithPreview(pageType, previewName string) string {
	return r.ResolveTemplateWithPreviewData(pageType, previewName, nil)
}

// ResolveTemplateWithPreviewData 根据页面类型和当前模板数据解析更细粒度的模板层级。
// 例如 page-about.gohtml、post-123.gohtml、category-go.gohtml 会优先于通用模板。
func (r *Renderer) ResolveTemplateWithPreviewData(pageType, previewName string, data any) string {
	r.mu.RLock()
	tpl := r.tpl
	if previewName != "" && r.previewTpl != nil && previewName == r.previewThemeName {
		tpl = r.previewTpl
	}
	r.mu.RUnlock()
	return resolveTemplateIn(tpl, pageType, data)
}

func resolveTemplateIn(tpl *template.Template, pageType string, data any) string {
	chain := templateHierarchyFor(pageType, data)
	if len(chain) == 0 {
		chain = TemplateHierarchy[pageType]
	}
	if len(chain) == 0 {
		chain = []string{pageType + ".gohtml"}
	}
	for _, name := range chain {
		if tpl != nil && tpl.Lookup(name) != nil {
			return name
		}
	}
	return chain[0]
}

func templateHierarchyFor(pageType string, data any) []string {
	chain, ok := TemplateHierarchy[pageType]
	if !ok {
		return []string{pageType + ".gohtml"}
	}
	result := make([]string, 0, len(chain)+2)
	switch pageType {
	case "post":
		post := dataValue(data, "Post")
		if slug := safeTemplateSegment(reflectStringField(post, "Slug")); slug != "" {
			result = append(result, "post-"+slug+".gohtml")
		}
		if id := reflectUintField(post, "ID"); id > 0 {
			result = append(result, fmt.Sprintf("post-%d.gohtml", id))
		}
	case "page":
		post := dataValue(data, "Post")
		if slug := safeTemplateSegment(reflectStringField(post, "Slug")); slug != "" {
			result = append(result, "page-"+slug+".gohtml")
		}
		if id := reflectUintField(post, "ID"); id > 0 {
			result = append(result, fmt.Sprintf("page-%d.gohtml", id))
		}
	case "category":
		if slug := safeTemplateSegment(firstDataString(data, "CategorySlug", "TermSlug", "Slug")); slug != "" {
			result = append(result, "category-"+slug+".gohtml")
		}
	case "tag":
		if slug := safeTemplateSegment(firstDataString(data, "TagSlug", "TermSlug", "Slug")); slug != "" {
			result = append(result, "tag-"+slug+".gohtml")
		}
	case "archive":
		if year := firstDataInt(data, "ArchiveYear", "Year"); year > 0 {
			result = append(result, fmt.Sprintf("archive-%d.gohtml", year))
		}
	}
	result = append(result, chain...)
	return uniqueStrings(result)
}

func firstDataString(data any, keys ...string) string {
	for _, key := range keys {
		if value, _ := dataValue(data, key).(string); value != "" {
			return value
		}
	}
	return ""
}

func firstDataInt(data any, keys ...string) int {
	for _, key := range keys {
		switch value := dataValue(data, key).(type) {
		case int:
			return value
		case int64:
			return int(value)
		case uint:
			return int(value)
		case uint64:
			return int(value)
		}
	}
	return 0
}

func safeTemplateSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) {
		return ""
	}
	return s
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
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
	hookInvokeProvider   atomic.Value // stores hookInvokeProviderHolder
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

type hookInvokeProviderHolder struct {
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

// SetHookInvokeProvider 设置 hookInvoke 模板函数的实现（由 theme.Manager 注入）。
func (r *Renderer) SetHookInvokeProvider(fn func(ctx *RequestContext, name string, args ...any) any) {
	if r == nil {
		return
	}
	r.themeRuntime.hookInvokeProvider.Store(hookInvokeProviderHolder{fn: fn})
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

func hookInvoke(runtime *TemplateRuntime, ctx *RequestContext, name string, args ...any) any {
	if runtime == nil {
		return nil
	}
	provider, _ := runtime.hookInvokeProvider.Load().(hookInvokeProviderHolder)
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
		if loader, _ := ctx.ThemeLoader.(*store.DataLoader); loader != nil {
			req = hook.WithDataLoader(req, loader)
		}
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

func postTitle(runtime *TemplateRuntime, ctx *RequestContext, post any) template.HTML {
	title := reflectStringField(post, "Title")
	if h := hooks(runtime); h != nil {
		title = stringFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.HookPostTitle, title, post), title)
	}
	title = html.EscapeString(title)
	if title == "" {
		return ""
	}
	if isDraft(ctx) {
		title += ` <span class="draft-badge">` + translate(ctx, "草稿预览") + `</span>`
	}
	return template.HTML(`<h1 class="post-title">` + title + `</h1>`)
}

func postExcerpt(runtime *TemplateRuntime, ctx *RequestContext, post any) template.HTML {
	html := postExcerptHTMLAny(post)
	if h := hooks(runtime); h != nil {
		html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.HookPostExcerptHTML, string(html), post))
	}
	return html
}

func postTags(post any) template.HTML {
	tags := reflectSliceField(post, "Tags")
	if len(tags) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<div class="post-tags">`)
	for _, tag := range tags {
		name := html.EscapeString(reflectStringField(tag, "Name"))
		slug := reflectStringField(tag, "Slug")
		if name == "" || slug == "" {
			continue
		}
		out.WriteString(`<a class="tag" href="`)
		out.WriteString(html.EscapeString(permalink.Tag(slug)))
		out.WriteString(`">#`)
		out.WriteString(name)
		out.WriteString(`</a>`)
	}
	out.WriteString(`</div>`)
	return template.HTML(out.String())
}

func postNavigation(ctx *RequestContext, data any, classes ...string) template.HTML {
	if data == nil && ctx != nil {
		data = ctx.Data
	}
	prev := dataValue(data, "PrevPost")
	next := dataValue(data, "NextPost")
	if isNilAny(prev) && isNilAny(next) {
		return ""
	}
	class := "post-nav"
	if len(classes) > 0 && strings.TrimSpace(classes[0]) != "" {
		class = strings.TrimSpace(classes[0])
	}
	var out strings.Builder
	out.WriteString(`<nav class="` + html.EscapeString(class) + `">`)
	out.WriteString(`<span class="prev">`)
	if !isNilAny(prev) {
		out.WriteString(`<a href="` + html.EscapeString(postURL(prev)) + `">← ` + html.EscapeString(reflectStringField(prev, "Title")) + `</a>`)
	}
	out.WriteString(`</span><span class="next">`)
	if !isNilAny(next) {
		out.WriteString(`<a href="` + html.EscapeString(postURL(next)) + `">` + html.EscapeString(reflectStringField(next, "Title")) + ` →</a>`)
	}
	out.WriteString(`</span></nav>`)
	return template.HTML(out.String())
}

func isNilAny(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func renderMenu(ctx *RequestContext, location string, data ...any) template.HTML {
	source := any(nil)
	if len(data) > 0 {
		source = data[0]
	}
	if source == nil && ctx != nil {
		source = ctx.Data
	}
	items := menuItemsForLocation(source, location)
	if len(items) == 0 {
		return ""
	}
	class := "menu"
	if location != "" {
		class += " menu-" + slugClass(location)
	}
	var out strings.Builder
	out.WriteString(`<nav class="` + html.EscapeString(class) + `">`)
	out.WriteString(`<ul class="menu-items">`)
	for _, item := range items {
		title := html.EscapeString(reflectStringField(item, "Title"))
		url := reflectStringField(item, "URL")
		if url == "" {
			url = postURL(item)
		}
		if title == "" || url == "" {
			continue
		}
		target := reflectStringField(item, "Target")
		out.WriteString(`<li class="menu-item"><a href="`)
		out.WriteString(html.EscapeString(url))
		out.WriteString(`"`)
		if target != "" {
			out.WriteString(` target="` + html.EscapeString(target) + `" rel="noreferrer"`)
		}
		out.WriteString(`>`)
		out.WriteString(title)
		out.WriteString(`</a>`)
		children := reflectSliceField(item, "Children")
		if len(children) > 0 {
			out.WriteString(`<ul class="sub-menu">`)
			for _, child := range children {
				writeMenuItem(&out, child)
			}
			out.WriteString(`</ul>`)
		}
		out.WriteString(`</li>`)
	}
	out.WriteString(`</ul></nav>`)
	return template.HTML(out.String())
}

func writeMenuItem(out *strings.Builder, item any) {
	title := html.EscapeString(reflectStringField(item, "Title"))
	url := reflectStringField(item, "URL")
	if title == "" || url == "" {
		return
	}
	target := reflectStringField(item, "Target")
	out.WriteString(`<li class="menu-item"><a href="`)
	out.WriteString(html.EscapeString(url))
	out.WriteString(`"`)
	if target != "" {
		out.WriteString(` target="` + html.EscapeString(target) + `" rel="noreferrer"`)
	}
	out.WriteString(`>`)
	out.WriteString(title)
	out.WriteString(`</a>`)
	children := reflectSliceField(item, "Children")
	if len(children) > 0 {
		out.WriteString(`<ul class="sub-menu">`)
		for _, child := range children {
			writeMenuItem(out, child)
		}
		out.WriteString(`</ul>`)
	}
	out.WriteString(`</li>`)
}

func menuItemsForLocation(data any, location string) []any {
	if location != "" {
		if menus := dataValue(data, "Menus"); menus != nil {
			if items := menuLocationItems(menus, location); len(items) > 0 {
				return items
			}
		}
		key := "Menu" + strings.ToUpper(location[:1]) + location[1:]
		if items := anySlice(dataValue(data, key)); len(items) > 0 {
			return items
		}
	}
	return anySlice(dataValue(data, "Menu"))
}

func menuLocationItems(menus any, location string) []any {
	rv := reflect.ValueOf(menus)
	for rv.IsValid() && (rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer) {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil
	}
	v := rv.MapIndex(reflect.ValueOf(location))
	if !v.IsValid() || !v.CanInterface() {
		return nil
	}
	return anySlice(v.Interface())
}

func anySlice(v any) []any {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && (rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer) {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil
	}
	items := make([]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if item.CanInterface() {
			items = append(items, item.Interface())
		}
	}
	return items
}

func bodyClass(data any) string {
	classes := []string{"site"}
	if pageType, _ := dataValue(data, "PageType").(string); pageType != "" {
		classes = append(classes, "page-type-"+slugClass(pageType))
	}
	if dataValue(data, "Post") != nil {
		classes = append(classes, "singular")
	}
	if dataValue(data, "List") != nil {
		classes = append(classes, "archive-list")
	}
	if uid, _ := dataValue(data, "CurrentUserID").(uint); uid != 0 {
		classes = append(classes, "logged-in")
	}
	if preview, _ := dataValue(data, "IsPreview").(bool); preview {
		classes = append(classes, "theme-preview")
	}
	return strings.Join(uniqueClasses(classes), " ")
}

func postClass(post any, extra ...string) string {
	classes := append([]string{"post"}, extra...)
	if id := reflectUintField(post, "ID"); id > 0 {
		classes = append(classes, fmt.Sprintf("post-%d", id))
	}
	if postType := reflectStringField(post, "PostType"); postType != "" {
		classes = append(classes, "type-"+slugClass(postType))
	}
	if status := reflectStringField(post, "Status"); status != "" {
		classes = append(classes, "status-"+slugClass(status))
	}
	for _, cat := range reflectSliceField(post, "Categories") {
		if slug := reflectStringField(cat, "Slug"); slug != "" {
			classes = append(classes, "category-"+slugClass(slug))
		}
	}
	for _, tag := range reflectSliceField(post, "Tags") {
		if slug := reflectStringField(tag, "Slug"); slug != "" {
			classes = append(classes, "tag-"+slugClass(slug))
		}
	}
	return strings.Join(uniqueClasses(classes), " ")
}

func commentClass(comment any, extra ...string) string {
	classes := append([]string{"comment"}, extra...)
	if id := reflectUintField(comment, "ID"); id > 0 {
		classes = append(classes, fmt.Sprintf("comment-%d", id))
	}
	if parentID := reflectUintField(comment, "ParentID"); parentID > 0 {
		classes = append(classes, "comment-reply")
	} else {
		classes = append(classes, "comment-root")
	}
	if status := reflectStringField(comment, "Status"); status != "" {
		classes = append(classes, "status-"+slugClass(status))
	}
	return strings.Join(uniqueClasses(classes), " ")
}

func slugClass(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			lastDash = r == '-'
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func uniqueClasses(classes []string) []string {
	seen := make(map[string]bool, len(classes))
	out := make([]string, 0, len(classes))
	for _, class := range classes {
		class = strings.TrimSpace(class)
		if class == "" || seen[class] {
			continue
		}
		seen[class] = true
		out = append(out, class)
	}
	return out
}

func isDraft(ctx *RequestContext) bool {
	if ctx == nil {
		return false
	}
	value, _ := dataValue(ctx.Data, "IsDraft").(bool)
	return value
}

func translate(ctx *RequestContext, msg string) string {
	if ctx == nil {
		return html.EscapeString(msg)
	}
	tr := dataValue(ctx.Data, "th")
	rv := reflect.ValueOf(tr)
	if !rv.IsValid() {
		return html.EscapeString(msg)
	}
	method := rv.MethodByName("T")
	if !method.IsValid() || method.Type().NumIn() != 1 || method.Type().In(0).Kind() != reflect.String || method.Type().NumOut() != 1 || method.Type().Out(0).Kind() != reflect.String {
		return html.EscapeString(msg)
	}
	out := method.Call([]reflect.Value{reflect.ValueOf(msg)})
	return html.EscapeString(out[0].String())
}

// postContent 输出文章正文，并应用 post.content_html filter。
func postContent(runtime *TemplateRuntime, ctx *RequestContext, post any) template.HTML {
	html := postContentHTML(post)
	if h := hooks(runtime); h != nil {
		html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.HookPostContentHTML, string(html), post))
	}
	return html
}

func postExcerptHTMLAny(post any) template.HTML {
	switch p := post.(type) {
	case *model.Post:
		if p == nil {
			return ""
		}
		return postExcerptHTML(p)
	case model.Post:
		return postExcerptHTML(&p)
	default:
		content := reflectStringField(post, "Content")
		excerpt := reflectStringField(post, "Excerpt")
		above, hasMore := wxr.SplitMore(content)
		switch {
		case hasMore:
			return template.HTML(addSrcSet(HighlightCodeBlocks(SanitizeHTML(above))))
		case excerpt != "":
			return template.HTML(addSrcSet(HighlightCodeBlocks(SanitizeHTML(excerpt))))
		case content != "":
			return template.HTML(addSrcSet(HighlightCodeBlocks(SanitizeHTML(content))))
		default:
			return ""
		}
	}
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
		html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.HookCommentContentHTML, string(html), comment))
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

func stringFromFilterValue(v any, fallback string) string {
	switch val := v.(type) {
	case string:
		return val
	case template.HTML:
		return string(val)
	case []byte:
		return string(val)
	default:
		return fallback
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

func reflectSliceField(v any, name string) []any {
	rv := indirectValue(v)
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return nil
	}
	f := rv.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.Slice {
		return nil
	}
	items := make([]any, 0, f.Len())
	for i := 0; i < f.Len(); i++ {
		item := f.Index(i)
		if item.CanInterface() {
			items = append(items, item.Interface())
		}
	}
	return items
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
				html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.HookWidgetRenderHTML, string(html), widget, area, data))
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
