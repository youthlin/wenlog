package theme

import "strings"

// WidgetTemplateName 返回主题可覆盖的 widget 模板名。
// 例如 recent_posts 对应 {{define "widget_recent_posts"}}。
func WidgetTemplateName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "-", "_")
	return "widget_" + name
}
