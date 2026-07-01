package theme

import (
	"context"
	"encoding/json"
	"html/template"
	"log/slog"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/render"
	gettext "github.com/youthlin/t"
)

const (
	// WidgetSourceBuiltin 表示内置组件。
	WidgetSourceBuiltin = "builtin"
	// WidgetSourceTheme 表示主题提供的组件。
	WidgetSourceTheme = "theme"
	// WidgetSourcePlugin 表示插件提供的组件。
	WidgetSourcePlugin = "plugin"
)

// BuiltinWidgetIDs 内置组件 ID 列表。
var BuiltinWidgetIDs = []string{"user_info", "search", "recent_posts", "recent_comments", "categories", "tag_cloud", "archive_months", "custom_html"}

// BuiltinWidgetDecls 内置组件声明。主题只需要声明组件区域即可使用这些组件；
// 若主题在 theme.yaml 中声明了同 ID 组件，则以主题声明为准。
var BuiltinWidgetDecls = []WidgetDecl{
	{ID: "user_info", Label: gettext.Mark.T("用户信息"), Options: []OptionDecl{
		{ID: "title", Type: "text", Label: gettext.Mark.T("标题"), Description: gettext.Mark.T("留空时根据登录状态显示欢迎语")},
	}},
	{ID: "search", Label: gettext.Mark.T("搜索"), Options: []OptionDecl{
		{ID: "title", Type: "text", Label: gettext.Mark.T("标题"), Default: gettext.Mark.T("搜索")},
	}},
	{ID: "recent_posts", Label: gettext.Mark.T("近期文章"), Options: []OptionDecl{
		{ID: "title", Type: "text", Label: gettext.Mark.T("标题"), Default: gettext.Mark.T("近期文章")},
		{ID: "count", Type: "number", Label: gettext.Mark.T("显示数量"), Default: "5", Min: floatPtr(1), Max: floatPtr(20)},
	}},
	{ID: "recent_comments", Label: gettext.Mark.T("近期评论"), Options: []OptionDecl{
		{ID: "title", Type: "text", Label: gettext.Mark.T("标题"), Default: gettext.Mark.T("近期评论")},
		{ID: "count", Type: "number", Label: gettext.Mark.T("显示数量"), Default: "5", Min: floatPtr(1), Max: floatPtr(20)},
	}},
	{ID: "categories", Label: gettext.Mark.T("分类目录"), Options: []OptionDecl{
		{ID: "title", Type: "text", Label: gettext.Mark.T("标题"), Default: gettext.Mark.T("分类目录")},
	}},
	{ID: "tag_cloud", Label: gettext.Mark.T("标签云"), Options: []OptionDecl{
		{ID: "title", Type: "text", Label: gettext.Mark.T("标题"), Default: gettext.Mark.T("标签")},
	}},
	{ID: "archive_months", Label: gettext.Mark.T("归档"), Options: []OptionDecl{
		{ID: "title", Type: "text", Label: gettext.Mark.T("标题"), Default: gettext.Mark.T("归档")},
	}},
	{ID: "custom_html", Label: gettext.Mark.T("自定义 HTML"), Options: []OptionDecl{
		{ID: "title", Type: "text", Label: gettext.Mark.T("标题")},
		{ID: "html", Type: "textarea", Label: gettext.Mark.T("HTML 内容"), Description: gettext.Mark.T("任意 HTML 代码，会原样输出到页面中")},
	}},
}

// IsBuiltinWidget 判断是否为内置组件。
func IsBuiltinWidget(id string) bool {
	return slices.Contains(BuiltinWidgetIDs, id)
}

// TemplateWidget 是基于模板渲染的组件适配器，内置组件和主题组件使用此实现。
type TemplateWidget struct {
	decl hook.WidgetDecl
}

// NewTemplateWidget 创建模板型组件。
func NewTemplateWidget(decl hook.WidgetDecl) *TemplateWidget {
	return &TemplateWidget{decl: decl}
}

func (w *TemplateWidget) Meta() hook.WidgetDecl { return w.decl }

func (w *TemplateWidget) Render(ctx context.Context, tpl *template.Template, instance hook.WidgetInstance, data any) (template.HTML, error) {
	if tpl == nil {
		return "", nil
	}
	var buf strings.Builder
	name := "widget_" + w.decl.ID
	if err := tpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

// RegisterBuiltins 注册所有内置组件到注册表。
func RegisterBuiltins(r *hook.WidgetRegistry) {
	for _, decl := range BuiltinWidgetDecls {
		decl.Source = WidgetSourceBuiltin
		r.Register(NewTemplateWidget(decl))
	}
}

// RegisterThemeWidgets 注册主题声明的组件到注册表。
func RegisterThemeWidgets(widgetRegistry *hook.WidgetRegistry, t *Theme) {
	if t == nil {
		return
	}
	for _, decl := range t.Widgets {
		if decl.ID == "" {
			continue
		}
		decl.Source = WidgetSourceTheme
		w := NewTemplateWidget(decl)
		widgetRegistry.Register(w)
	}
}

// WidgetDeclsWithBuiltins 返回主题组件声明与内置组件声明的合并结果。
func WidgetDeclsWithBuiltins(t *Theme) []WidgetDecl {
	seen := make(map[string]bool)
	var decls []WidgetDecl
	if t != nil {
		for _, w := range t.Widgets {
			if w.ID == "" || seen[w.ID] {
				continue
			}
			w.Source = WidgetSourceTheme
			decls = append(decls, w)
			seen[w.ID] = true
		}
	}
	for _, w := range BuiltinWidgetDecls {
		if seen[w.ID] {
			continue
		}
		w.Source = WidgetSourceBuiltin
		decls = append(decls, w)
		seen[w.ID] = true
	}
	return decls
}

// WidgetDeclsWithPlugins 返回主题、内置和当前启用插件的组件声明列表。
func WidgetDeclsWithPlugins(t *Theme, pluginWidgets []WidgetDecl) []WidgetDecl {
	decls := WidgetDeclsWithBuiltins(t)
	for _, w := range pluginWidgets {
		if w.ID == "" {
			continue
		}
		w.Source = WidgetSourcePlugin
		decls = append(decls, w)
	}
	return normalizeWidgetDecls(decls)
}

// WidgetInfo 渲染时使用的组件信息。
type WidgetInfo = render.WidgetInfo

// WidgetConfigItem 是组件配置对象数组中的一个条目。
type WidgetConfigItem struct {
	InstanceID string            `json:"instance_id,omitempty"`
	ID         string            `json:"id"`
	Source     string            `json:"source,omitempty"`
	Opts       map[string]string `json:"opts,omitempty"`
}

// ResolveWidgetsWithDecls 根据用户配置和已过滤的组件声明解析区域组件列表。
func ResolveWidgetsWithDecls(userConfigJSON string, t *Theme, area string, decls []WidgetDecl) []WidgetInfo {
	if t == nil {
		return nil
	}
	decls = normalizeWidgetDecls(decls)
	available := widgetsInAreaFromDecls(t, area, decls)
	if len(available) == 0 {
		return nil
	}
	items := ParseWidgetConfig(userConfigJSON)
	if len(items) == 0 {
		return nil
	}

	var result []WidgetInfo
	for i, item := range items {
		decl, ok := resolveWidgetDecl(available, item)
		if !ok {
			slog.Info("widget resolve failed", "area", area, "item_id", item.ID, "item_source", item.Source)
			continue
		}
		if decl.Source != WidgetSourcePlugin && !widgetTemplateExists(t, decl.ID) && !IsBuiltinWidget(decl.ID) {
			slog.Info("widget template missing", "area", area, "id", decl.ID, "source", decl.Source)
			continue
		}
		slog.Info("widget resolved", "area", area, "id", decl.ID, "source", decl.Source, "plugin_id", decl.PluginID)
		result = append(result, WidgetInfo{
			InstanceID:   widgetInstanceID(item, i),
			ID:           decl.ID,
			Source:       decl.Source,
			PluginID:     decl.PluginID,
			TemplateName: "widget_" + decl.ID,
			Options:      item.Opts,
		})
	}
	return result
}

func widgetInstanceID(item WidgetConfigItem, index int) string {
	if strings.TrimSpace(item.InstanceID) != "" {
		return item.InstanceID
	}
	return strings.ReplaceAll(widgetConfigKey(item.Source, item.ID), ":", "-") + "-" + strconv.Itoa(index)
}

// ResolveWidgets 根据用户配置和主题声明，解析某个区域应渲染的组件列表。
// userConfigJSON 是 Setting 表中存储的 JSON, 格式:
//   - 对象数组格式: `[{"id":"search"},{"id":"recent_posts","opts":{"count":"10"}}]`
//
// 为空表示用户尚未配置该区域，不渲染任何组件。
func ResolveWidgets(userConfigJSON string, t *Theme, area string) []WidgetInfo {
	return ResolveWidgetsWithDecls(userConfigJSON, t, area, WidgetDeclsWithBuiltins(t))
}

func widgetsInAreaFromDecls(t *Theme, area string, decls []WidgetDecl) map[string]WidgetDecl {
	if t == nil {
		return nil
	}
	available := make(map[string]WidgetDecl)
	if _, ok := t.WidgetAreas[area]; !ok && area != "" {
		return available
	}
	for _, w := range decls {
		if w.ID == "" {
			continue
		}
		available[widgetDeclKey(w)] = w
	}
	return available
}

func resolveWidgetDecl(available map[string]WidgetDecl, item WidgetConfigItem) (WidgetDecl, bool) {
	if item.ID == "" {
		return WidgetDecl{}, false
	}
	if item.Source == "" {
		return WidgetDecl{}, false
	}
	if decl, ok := available[widgetConfigKey(item.Source, item.ID)]; ok {
		return decl, true
	}
	return WidgetDecl{}, false
}

// ParseWidgetConfig 解析用户配置 JSON。
func ParseWidgetConfig(userConfigJSON string) []WidgetConfigItem {
	if userConfigJSON == "" {
		return nil
	}
	var items []WidgetConfigItem
	if err := json.Unmarshal([]byte(userConfigJSON), &items); err == nil {
		return items
	}
	return nil
}

// HasLegacyWidgetConfig 判断配置中是否存在缺少 source 的旧格式条目。
// 插件化后组件 ID 不再全局唯一，旧格式无法可靠判断来源，因此需要清空后重新配置。
func HasLegacyWidgetConfig(userConfigJSON string) bool {
	items := ParseWidgetConfig(userConfigJSON)
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if item.ID != "" && strings.TrimSpace(item.Source) == "" {
			return true
		}
	}
	return false
}

// widgetTemplateExists 检查主题是否提供了指定组件的模板文件。
func widgetTemplateExists(t *Theme, id string) bool {
	return t != nil && t.WidgetTemplates[id]
}

// MissingWidgets 返回用户配置中存在但当前主题未声明的组件 ID 列表。
func MissingWidgets(userConfigJSON string, t *Theme) []string {
	items := ParseWidgetConfig(userConfigJSON)
	if len(items) == 0 {
		return nil
	}
	available := make(map[string]bool)
	for _, w := range WidgetDeclsWithBuiltins(t) {
		available[w.ID] = true
	}
	var missing []string
	seen := make(map[string]bool)
	for _, item := range items {
		if !available[item.ID] && !seen[item.ID] {
			missing = append(missing, item.ID)
			seen[item.ID] = true
		}
	}
	sort.Strings(missing)
	return missing
}

func normalizeWidgetDecls(decls []WidgetDecl) []WidgetDecl {
	seen := make(map[string]bool, len(decls))
	out := make([]WidgetDecl, 0, len(decls))
	for _, w := range decls {
		if w.ID == "" {
			continue
		}
		w.Source = normalizeWidgetSource(w.Source)
		key := widgetDeclKey(w)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, w)
	}
	return out
}

func normalizeWidgetSource(source string) string {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "plugin:") {
		return WidgetSourcePlugin
	}
	switch source {
	case WidgetSourceBuiltin, WidgetSourceTheme, WidgetSourcePlugin:
		return source
	default:
		return WidgetSourceTheme
	}
}

func pluginIDFromSource(source string) string {
	if strings.HasPrefix(source, "plugin:") {
		return strings.TrimPrefix(source, "plugin:")
	}
	return ""
}

func widgetDeclKey(w WidgetDecl) string {
	if w.Source == WidgetSourcePlugin && w.PluginID != "" {
		return WidgetSourcePlugin + ":" + w.PluginID + ":" + w.ID
	}
	return w.Source + ":" + w.ID
}

func widgetConfigKey(source, id string) string {
	if normalizeWidgetSource(source) == WidgetSourcePlugin {
		if pluginID := pluginIDFromSource(source); pluginID != "" {
			return WidgetSourcePlugin + ":" + pluginID + ":" + id
		}
	}
	return normalizeWidgetSource(source) + ":" + id
}

func floatPtr(v float64) *float64 {
	return &v
}
