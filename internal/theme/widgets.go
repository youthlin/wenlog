package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// BuiltinWidgetIDs 内置组件 ID 列表。
var BuiltinWidgetIDs = []string{"search", "recent_posts", "categories", "tag_cloud", "recent_comments", "custom_html", "user_info"}

// IsBuiltinWidget 判断是否为内置组件。
func IsBuiltinWidget(id string) bool {
	for _, bid := range BuiltinWidgetIDs {
		if bid == id {
			return true
		}
	}
	return false
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
// userConfigJSON 是 Setting 表中存储的 JSON，支持两种格式：
//   - 旧格式: `["search","recent_posts"]`
//   - 对象数组格式: `[{"id":"search"},{"id":"recent_posts","opts":{"count":"10"}}]`
//
// 为空则使用主题默认。
func ResolveWidgets(userConfigJSON string, t *Theme, area string) []WidgetInfo {
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

// ParseWidgetConfig 解析用户配置 JSON，兼容新旧两种格式。
func ParseWidgetConfig(userConfigJSON string) []WidgetConfigItem {
	if userConfigJSON == "" {
		return nil
	}
	// 先尝试对象数组格式: [{"id":"x","opts":{...}}]
	var items []WidgetConfigItem
	if err := json.Unmarshal([]byte(userConfigJSON), &items); err == nil {
		return items
	}
	// 回退到旧格式: ["id1","id2"]
	var ids []string
	if err := json.Unmarshal([]byte(userConfigJSON), &ids); err == nil {
		items = make([]WidgetConfigItem, len(ids))
		for i, id := range ids {
			items[i] = WidgetConfigItem{ID: id}
		}
		return items
	}
	return nil
}

// widgetTemplateExists 检查主题是否提供了指定组件的模板文件。
func widgetTemplateExists(t *Theme, id string) bool {
	path := filepath.Join(t.WidgetsDir(), id+".gohtml")
	_, err := os.Stat(path)
	return err == nil
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
