// Package wxr — 内容清洗:把 WordPress 正文转为干净 HTML。
package wxr

import (
	"regexp"
	"strings"
)

var (
	// Gutenberg 区块注释:<!-- wp:xxx ... --> 和 <!-- /wp:xxx -->。
	// 注意不能误删 <!--more--> (它没有 wp: 前缀)。
	gutenbergRe = regexp.MustCompile(`<!--\s*/?wp:[^>]*?-->`)

	// [caption ...]<inner>[/caption] 短代码。
	captionRe = regexp.MustCompile(`(?s)\[caption[^\]]*\](.*?)\[/caption\]`)

	// 协议相对 URL //youthlin.com → https://youthlin.com 之前先处理为站内绝对路径。
	protoRelRe = regexp.MustCompile(`(["'(])//youthlin\.com`)

	// 连续 3 个以上空行压缩为 2 个。
	multiBlankRe = regexp.MustCompile(`\n{3,}`)
)

// MoreMarker 是 WordPress 的 “阅读更多” 分割标记。
const MoreMarker = "<!--more-->"

// CleanContent 清洗正文 HTML:去除 Gutenberg 注释、转换 caption 短代码、
// 归一化协议相对 URL。保留 <!--more--> 标记。
func CleanContent(html string) string {
	// 1. caption 短代码 → figure(保留内部的 <a>/<img>,把尾随文字作为 figcaption)。
	html = captionRe.ReplaceAllStringFunc(html, func(m string) string {
		inner := captionRe.FindStringSubmatch(m)[1]
		inner = strings.TrimSpace(inner)
		// 内部通常是 <a><img></a> + 空格 + 说明文字。尝试分离尾部文字。
		caption := ""
		if idx := strings.LastIndex(inner, ">"); idx >= 0 && idx < len(inner)-1 {
			caption = strings.TrimSpace(inner[idx+1:])
			inner = inner[:idx+1]
		}
		if caption != "" {
			return "<figure>" + inner + "<figcaption>" + caption + "</figcaption></figure>"
		}
		return "<figure>" + inner + "</figure>"
	})

	// 2. 去除 Gutenberg 区块注释(保留 <!--more-->)。
	html = gutenbergRe.ReplaceAllString(html, "")

	// 3. 协议相对 URL 归一化为 https。
	html = protoRelRe.ReplaceAllString(html, "${1}https://youthlin.com")

	// 4. 压缩多余空行。
	html = multiBlankRe.ReplaceAllString(html, "\n\n")

	return strings.TrimSpace(html)
}

// SplitMore 按 <!--more--> 拆分正文,返回 (more 之前内容, 是否含 more)。
// 用于列表页只展示开头部分。
func SplitMore(content string) (above string, hasMore bool) {
	idx := strings.Index(content, MoreMarker)
	if idx < 0 {
		return content, false
	}
	return strings.TrimSpace(content[:idx]), true
}

// RenderDetail 把正文中的 <!--more--> 替换为锚点,用于详情页输出完整内容。
func RenderDetail(content string, postID uint) string {
	anchor := `<span id="more-` + itoa(postID) + `"></span>`
	return strings.Replace(content, MoreMarker, anchor, 1)
}

func itoa(n uint) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
