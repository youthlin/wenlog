package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// BuiltinWidgetIDs 内置组件 ID 列表。
var BuiltinWidgetIDs = []string{"search", "recent_posts", "categories", "tag_cloud", "recent_comments"}

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
	ID           string // 组件 ID
	TemplateName string // 模板 define 名称，如 "widget_search"
}

// ResolveWidgets 根据用户配置和主题声明，解析某个区域应渲染的组件列表。
// userConfigJSON 是 Setting 表中存储的 JSON 数组（如 `["search","recent_posts"]`），为空则使用主题默认。
// theme 是当前主题，area 是区域名（如 "sidebar"）。
// 返回按序排列的组件信息列表，缺失模板的组件会被跳过。
func ResolveWidgets(userConfigJSON string, t *Theme, area string) []WidgetInfo {
	var ids []string
	if userConfigJSON != "" {
		if err := json.Unmarshal([]byte(userConfigJSON), &ids); err != nil {
			ids = nil
		}
	}
	if len(ids) == 0 {
		// 使用主题默认：该区域声明的全部组件，按 yaml 声明顺序
		for _, w := range t.Widgets {
			if w.Area == area {
				ids = append(ids, w.ID)
			}
		}
	}

	// 构建当前主题可用组件集合
	available := make(map[string]bool)
	for _, w := range t.Widgets {
		available[w.ID] = true
	}

	var result []WidgetInfo
	for _, id := range ids {
		// 检查模板是否存在：主题 widgets/ 目录 > 内置 web/widgets/
		tmplName := "widget_" + id
		if !widgetTemplateExists(t, id) && !IsBuiltinWidget(id) {
			continue // 主题未提供且非内置，跳过
		}
		result = append(result, WidgetInfo{ID: id, TemplateName: tmplName})
	}
	return result
}

// widgetTemplateExists 检查主题是否提供了指定组件的模板文件。
func widgetTemplateExists(t *Theme, id string) bool {
	path := filepath.Join(t.WidgetsDir(), id+".gohtml")
	_, err := os.Stat(path)
	return err == nil
}

// MissingWidgets 返回用户配置中存在但当前主题未声明的组件 ID 列表。
func MissingWidgets(userConfigJSON string, t *Theme) []string {
	if userConfigJSON == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(userConfigJSON), &ids); err != nil {
		return nil
	}
	available := make(map[string]bool)
	for _, w := range t.Widgets {
		available[w.ID] = true
	}
	var missing []string
	for _, id := range ids {
		if !available[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing
}
