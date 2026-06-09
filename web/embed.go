// Package web 通过 embed 嵌入模板与静态资源,使最终二进制自包含。
package web

import "embed"

// Templates 是所有 HTML 模板。
//
//go:embed templates/*.gohtml
var Templates embed.FS

// Assets 是 CSS/JS 等前端静态资源。
//
//go:embed assets
var Assets embed.FS

// I18n 是内置翻译文件。
//
//go:embed i18n
var I18n embed.FS
