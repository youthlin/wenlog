// Package web 通过 embed 嵌入模板与静态资源,使最终二进制自包含。
package web

import "embed"

// Templates 是后台与认证相关的 HTML 模板（admin_*.gohtml、auth_*.gohtml）。
// 前台模板已移至 Themes。
//
//go:embed templates/*.gohtml
var Templates embed.FS

// Assets 是 CSS/JS 等前端静态资源。
//
//go:embed assets
var Assets embed.FS

// I18n 是后台/认证模板及 Go 代码的内置翻译文件。
// 前台模板翻译已移至 Themes。
//
//go:embed i18n
var I18n embed.FS

// Themes 是所有内嵌主题的完整内容（模板 + 翻译 + theme.yaml + assets）。
// 每个主题一个子目录，如 themes/default/、themes/single/。
//
//go:embed themes/*
var Themes embed.FS

// Widgets 是内置组件模板。
//
//go:embed widgets/*.gohtml
var Widgets embed.FS
