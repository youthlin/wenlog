// Package render 负责加载 HTML 模板、注册模板函数,并提供渲染辅助。
package render

import (
	"crypto/md5"
	"encoding/hex"
	stdhtml "html"
	"html/template"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/cockroachdb/errors"
	ginrender "github.com/gin-gonic/gin/render"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/wxr"
)

// Renderer 持有模板配置,既支持静态模板,也支持开发期热更新。
type Renderer struct {
	tpl     *template.Template
	fsys    fs.FS
	pattern string
	hot     bool
}

const pattern = "*.gohtml"

// New 从给定文件系统(通常是 embed.FS 的子目录)加载所有 *.gohtml 模板。
func New(fsys fs.FS) (*Renderer, error) {
	tpl, err := parseTemplates(fsys, pattern)
	if err != nil {
		return nil, err
	}
	return &Renderer{
		tpl:     tpl,
		fsys:    fsys,
		pattern: pattern,
	}, nil
}

// NewHot 从磁盘目录创建一个可热更新的模板渲染器。
// 每次 Render 都会重新 Parse 目录下的 *.gohtml,适合开发时微调模板无需重启。
func NewHot(dir string) (*Renderer, error) {
	fsys := os.DirFS(dir)
	tpl, err := parseTemplates(fsys, pattern)
	if err != nil {
		return nil, err
	}
	return &Renderer{
		tpl:     tpl,
		fsys:    fsys,
		pattern: pattern,
		hot:     true,
	}, nil
}

func parseTemplates(fsys fs.FS, pattern string) (*template.Template, error) {
	funcs := template.FuncMap{
		"postURL":          func(p *model.Post) string { return permalink.Post(p) },
		"pageURL":          func(p *model.Post) string { return permalink.Page(p) },
		"categoryURL":      permalink.Category,
		"tagURL":           permalink.Tag,
		"safeHTML":         func(s string) template.HTML { return template.HTML(s) },
		"listHTML":         listHTML,
		"detailHTML":       detailHTML,
		"hasMore":          func(content string) bool { _, m := wxr.SplitMore(content); return m },
		"gravatar":         gravatar,
		"gravatarPrimary":  gravatarPrimary,
		"gravatarFallback": gravatarFallback,
		"fmtDate":          func(t time.Time) string { return t.Format("2006-01-02") },
		"fmtDateTime":      func(t time.Time) string { return t.Format("2006-01-02 15:04") },
		"year":             func(t time.Time) int { return t.Year() },
		"add":              func(a, b int) int { return a + b },
		"sub":              func(a, b int) int { return a - b },
		"seq":              seq,
	}
	tpl, err := template.New("").Funcs(funcs).ParseFS(fsys, pattern)
	if err != nil {
		return nil, errors.Wrap(err, "parse templates")
	}
	return tpl, nil
}

// Template 返回底层模板(仅静态模板模式可用;热更新模式下会返回 nil)。
func (r *Renderer) Template() *template.Template { return r.tpl }

// Instance 实现 gin/render.HTMLRender。热更新模式下每次请求重新解析模板。
func (r *Renderer) Instance(name string, data any) ginrender.Render {
	tpl := r.tpl
	if r.hot {
		var err error
		tpl, err = parseTemplates(r.fsys, r.pattern)
		if err != nil {
			return ginrender.String{
				Format: "template parse error: %v",
				Data:   []any{err},
			}
		}
	}
	return ginrender.HTML{Template: tpl, Name: name, Data: data}
}

// listHTML 返回列表页应展示的正文 HTML:有 more 标记则只取之前部分。
func listHTML(p *model.Post) template.HTML {
	above, hasMore := wxr.SplitMore(p.Content)
	if hasMore {
		return template.HTML(HighlightCodeBlocks(above))
	}
	if p.Excerpt != "" {
		return template.HTML(HighlightCodeBlocks(p.Excerpt))
	}
	return template.HTML(HighlightCodeBlocks(p.Content))
}

// detailHTML 返回详情页完整正文,把 <!--more--> 替换为锚点。
func detailHTML(p *model.Post) template.HTML {
	return template.HTML(HighlightCodeBlocks(wxr.RenderDetail(p.Content, p.ID)))
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

// seq 生成 [1..n] 整数切片,供分页模板迭代。
func seq(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i + 1
	}
	return s
}

// gravatar 由邮箱生成 cravatar(国内镜像)头像 URL。
func gravatar(email string) string {
	return gravatarFallback(email)
}

func gravatarPrimary(email string) string {
	return "https://gravatar.com/avatar/" + avatarHash(email) + "?s=48"
}

func gravatarFallback(email string) string {
	return "https://cn.cravatar.com/avatar/" + avatarHash(email) + "?s=48&d=mp"
}

func avatarHash(email string) string {
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}
