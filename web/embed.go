// Package web 通过 embed 嵌入模板与静态资源,使最终二进制自包含。
package web

import "embed"

// Templates 是后台与认证相关的 HTML 模板（admin_*.gohtml、auth_*.gohtml）。
// 前台模板已移至 DefaultTheme。
//
//go:embed templates/*.gohtml
var Templates embed.FS

// Assets 是 CSS/JS 等前端静态资源。
//
//go:embed assets
var Assets embed.FS

// I18n 是后台/认证模板及 Go 代码的内置翻译文件。
// 前台模板翻译已移至 DefaultTheme。
//
//go:embed i18n
var I18n embed.FS

// DefaultTheme 是默认主题的完整内容（模板 + 翻译 + theme.yaml）。
//
//go:embed themes/default
var DefaultTheme embed.FS
