package render

import (
	"fmt"
	stdhtml "html"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/gomarkdown/markdown"
	mhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/microcosm-cc/bluemonday"
)

// RenderMarkdown 把 Markdown 渲染为 HTML,消毒后复用统一的代码块高亮逻辑。
func RenderMarkdown(md string) string {
	p := parser.NewWithExtensions(parser.CommonExtensions | parser.AutoHeadingIDs)
	doc := p.Parse([]byte(md))
	renderer := mhtml.NewRenderer(mhtml.RendererOptions{
		Flags: mhtml.CommonFlags,
	})
	out := string(markdown.Render(doc, renderer))
	return HighlightCodeBlocks(SanitizeHTML(out))
}

var htmlSanitizer = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").
		Matching(regexp.MustCompile(`^language-[A-Za-z0-9_+.-]+$`)).
		OnElements("code")
	return p
}()

// SanitizeHTML 清理用户可编辑/导入的正文 HTML,保留常见富文本但移除脚本与事件属性。
func SanitizeHTML(src string) string {
	return htmlSanitizer.Sanitize(src)
}

var fencedCodeRe = regexp.MustCompile(`(?s)<pre><code(?: class="language-([^"]+)")?>(.*?)</code></pre>`)

// HighlightCodeBlocks 为 HTML 中的 <pre><code> 做服务端高亮与行号输出。
func HighlightCodeBlocks(src string) string {
	return fencedCodeRe.ReplaceAllStringFunc(src, func(block string) string {
		m := fencedCodeRe.FindStringSubmatch(block)
		lang := strings.TrimSpace(m[1])
		code := stdhtml.UnescapeString(m[2])
		lexer := lexers.Get(lang)
		if lexer == nil {
			lexer = lexers.Analyse(code)
		}
		if lexer == nil {
			lexer = lexers.Fallback
		}
		it, err := lexer.Tokenise(nil, code)
		if err != nil {
			return block
		}
		formatter := html.New(
			html.WithClasses(false),
			html.WithLineNumbers(true),
			html.LineNumbersInTable(true),
		)
		style := styles.Get("github-dark")
		if style == nil {
			style = styles.Fallback
		}
		var b strings.Builder
		if err := formatter.Format(&b, style, it); err != nil {
			return block
		}
		return `<div class="codehilite">` + b.String() + `</div>`
	})
}

var (
	// imgSrcSetRe 匹配 <img ... src="/wp-content/uploads/..." ...> 标签。
	imgSrcSetRe = regexp.MustCompile(`<img\b([^>]*)\bsrc="(/wp-content/uploads/[^"]+)"([^>]*)>`)
	// wpThumbSuffixRe 匹配 WordPress 缩略图后缀（如 -150x150、-300x300、-768w）。
	wpThumbSuffixRe = regexp.MustCompile(`-\d+x\d+$|-\d+w$`)
)

// addSrcSet 为本地图片自动添加 srcset + sizes + loading=lazy 属性。
// 缩略图命名规则（WordPress 兼容）:
//
//	xxx.png → xxx-150x150.png (150w), xxx-300x300.png (300w), xxx-768w.png (768w)
func addSrcSet(html string) string {
	return imgSrcSetRe.ReplaceAllStringFunc(html, func(img string) string {
		m := imgSrcSetRe.FindStringSubmatch(img)
		if m == nil {
			return img
		}
		before := m[1]
		src := m[2]
		after := m[3]

		// 已有 srcset 则跳过
		if strings.Contains(before, "srcset=") || strings.Contains(after, "srcset=") {
			return img
		}

		// 已有 loading 则跳过
		hasLoading := strings.Contains(before, "loading=") || strings.Contains(after, "loading=")

		ext := filepath.Ext(src)
		// 如果 src 本身已经是缩略图（如 xxx-150x150.png），还原为原图路径
		base := wpThumbSuffixRe.ReplaceAllString(strings.TrimSuffix(src, ext), "")
		origURL := base + ext

		srcset := fmt.Sprintf(`%s-150x150%s 150w, %s-300x300%s 300w, %s-768w%s 768w, %s %dw`,
			base, ext, base, ext, base, ext, origURL, 1920)

		sizes := `sizes="(max-width: 150px) 150px, (max-width: 300px) 300px, (max-width: 768px) 768px, 100vw"`

		result := fmt.Sprintf(`<img%s src="%s"%s srcset="%s" %s`, before, src, after, srcset, sizes)
		if !hasLoading {
			result += ` loading="lazy"`
		}
		return result
	})
}
