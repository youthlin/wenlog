package render

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	ginrender "github.com/gin-gonic/gin/render"
	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/util"
	"github.com/youthlin/wenlog/web"
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
		return &themeHTMLRender{tmpl: previewTpl, name: name, data: data, runtime: &r.themeRuntime}
	}
	return &themeHTMLRender{tmpl: mainTpl, name: name, data: data, runtime: &r.themeRuntime}
}

func requestContext(r *RequestContext) context.Context {
	var ctx context.Context
	if r != nil && r.Context != nil {
		ctx = r.Context
	} else {
		ctx = context.Background()
	}
	if r != nil && r.ThemeLoader != nil {
		if loader, _ := r.ThemeLoader.(*store.DataLoader); loader != nil {
			ctx = hook.WithDataLoader(ctx, loader)
		}
	}
	return ctx
}

// 以下 reflect* 辅助集中服务于模板函数的“宽输入”能力：模板里传入的 post/comment/menu
// 可能来自 model.Post、hook.PostView、theme.MenuItem、匿名测试结构或 yaegi 脚本值。
// 为了不把模板函数签名收窄到某一个包的具体类型，这里只按固定字段白名单读取展示所需数据。
func reflectStringField(v any, name string) string {
	if m, ok := v.(map[string]any); ok {
		if s, _ := m[name].(string); s != "" {
			return s
		}
	}
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
	if m, ok := v.(map[string]any); ok {
		switch n := m[name].(type) {
		case uint:
			return n
		case int:
			if n > 0 {
				return uint(n)
			}
		}
	}
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
	if m, ok := v.(map[string]any); ok {
		return anySlice(m[name])
	}
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
func renderWidgets(ctx *RequestContext, area string, data any) template.HTML {
	if ctx == nil || ctx.Runtime == nil || ctx.Template == nil {
		return ""
	}

	provider := ctx.Runtime.current().WidgetsProvider
	if provider == nil {
		return ""
	}
	widgets := provider(ctx, area)
	if len(widgets) == 0 {
		slog.InfoContext(ctx.Context, "renderWidgets: no widgets for area", "area", area)
		return ""
	}
	slog.InfoContext(ctx.Context, "renderWidgets: rendering widgets", "area", area, "count", len(widgets))
	var result strings.Builder
	for _, item := range widgets {
		ctx.WidgetOptions = item.Options
		html, ok := renderWidgetItem(ctx, item, data)
		if !ok {
			slog.InfoContext(ctx.Context, "renderWidgets: widget render failed", "area", area, "id", item.ID, "source", item.Source, "plugin_id", item.PluginID)
			continue
		}
		if h := hooks(ctx.Runtime); h != nil {
			html = trustedHTMLFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.FilterWidgetRenderHTML, string(html), item, area, data))
		}
		util.WriteString(&result, string(html))
	}
	ctx.WidgetOptions = nil
	return template.HTML(result.String())
}

func renderWidgetItem(ctx *RequestContext, item WidgetInfo, data any) (template.HTML, bool) {
	resolver := ctx.Runtime.current().WidgetResolver
	if resolver == nil {
		slog.InfoContext(ctx.Context, "renderWidgetItem: no resolver")
		return "", false
	}
	w := resolver(item.Source, item.ID, item.PluginID)
	if w == nil {
		slog.InfoContext(ctx.Context, "renderWidgetItem: widget not found in registry", "source", item.Source, "id", item.ID, "plugin_id", item.PluginID)
		return "", false
	}
	slog.InfoContext(ctx.Context, "renderWidgetItem: widget found, rendering", "source", item.Source, "id", item.ID, "plugin_id", item.PluginID)
	instance := hook.WidgetInstance{
		InstanceID: item.InstanceID,
		WidgetID:   item.ID,
		Settings:   ctx.WidgetOptions,
	}
	html, err := w.Render(requestContext(ctx), ctx.Template, instance, data)
	if err != nil || html == "" {
		slog.InfoContext(ctx.Context, "renderWidgetItem: render returned empty/error", "id", item.ID, "error", err)
		return "", false
	}
	return html, true
}
