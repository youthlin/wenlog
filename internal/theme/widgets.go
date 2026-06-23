package theme

import (
	"encoding/json"
	"slices"
	"sort"

	gettext "github.com/youthlin/t"
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

// WidgetDeclsWithBuiltins 返回主题组件声明与内置组件声明的合并结果。
func WidgetDeclsWithBuiltins(t *Theme) []WidgetDecl {
	seen := make(map[string]bool)
	var decls []WidgetDecl
	if t != nil {
		for _, w := range t.Widgets {
			if w.ID == "" || seen[w.ID] {
				continue
			}
			decls = append(decls, w)
			seen[w.ID] = true
		}
	}
	for _, w := range BuiltinWidgetDecls {
		if seen[w.ID] {
			continue
		}
		decls = append(decls, w)
		seen[w.ID] = true
	}
	return decls
}

// WidgetInfo 渲染时使用的组件信息。
type WidgetInfo struct {
	ID           string            // 组件 ID
	TemplateName string            // 模板 define 名称，如 "widget_search"
	Options      map[string]string // 组件选项
}

// WidgetConfigItem 是组件配置对象数组中的一个条目。
type WidgetConfigItem struct {
	ID   string            `json:"id"`
	Opts map[string]string `json:"opts,omitempty"`
}

// ResolveWidgets 根据用户配置和主题声明，解析某个区域应渲染的组件列表。
// userConfigJSON 是 Setting 表中存储的 JSON, 格式:
//   - 对象数组格式: `[{"id":"search"},{"id":"recent_posts","opts":{"count":"10"}}]`
//
// 为空则使用主题默认。
func ResolveWidgets(userConfigJSON string, t *Theme, area string) []WidgetInfo {
	available := widgetsInArea(t, area)
	if len(available) == 0 {
		return nil
	}
	items := ParseWidgetConfig(userConfigJSON)
	if len(items) == 0 {
		// 使用主题默认：该区域声明的全部组件，按 yaml 声明顺序
		for _, w := range t.Widgets {
			if w.Area == area {
				items = append(items, WidgetConfigItem{ID: w.ID})
			}
		}
	}

	var result []WidgetInfo
	for _, item := range items {
		if !available[item.ID] {
			continue
		}
		tmplName := "widget_" + item.ID
		if !widgetTemplateExists(t, item.ID) && !IsBuiltinWidget(item.ID) {
			continue
		}
		result = append(result, WidgetInfo{
			ID:           item.ID,
			TemplateName: tmplName,
			Options:      item.Opts,
		})
	}
	return result
}

func widgetsInArea(t *Theme, area string) map[string]bool {
	if t == nil {
		return nil
	}
	available := make(map[string]bool)
	for _, w := range t.Widgets {
		if w.Area == area {
			available[w.ID] = true
		}
	}
	if _, ok := t.WidgetAreas[area]; ok || area == "" {
		for _, id := range BuiltinWidgetIDs {
			available[id] = true
		}
	}
	return available
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
	for _, w := range t.Widgets {
		available[w.ID] = true
	}
	if len(t.WidgetAreas) > 0 {
		for _, id := range BuiltinWidgetIDs {
			available[id] = true
		}
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

func floatPtr(v float64) *float64 {
	return &v
}
