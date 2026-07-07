// Package plugins 嵌入内置插件，使单一二进制可以在首次启动时释放插件目录。
package plugins

import "embed"

// Plugins 是所有内置插件的完整内容（plugin.yaml + functions + widgets + assets）。
// 每个插件一个子目录，如 post-comment-enhance/、common-widgets/。
//
//go:embed post-comment-enhance common-widgets
var Plugins embed.FS
