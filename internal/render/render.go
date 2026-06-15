// Package render 负责加载 HTML 模板、注册模板函数,并提供渲染辅助。
package render

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	stdhtml "html"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/cockroachdb/errors"
	ginrender "github.com/gin-gonic/gin/render"
	"github.com/microcosm-cc/bluemonday"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/wxr"
)

// Renderer 持有模板配置,既支持静态模板,也支持开发期热更新。
type Renderer struct {
	mu      sync.RWMutex
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
// 启动时会先 Parse 一次模板,之后只有显式调用 Reload 才会重新解析。
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

// Hot 返回当前渲染器是否处于本地模板热更新模式。
func (r *Renderer) Hot() bool { return r != nil && r.hot }

// Reload 重新解析模板并替换当前缓存。仅热更新模式支持该操作。
func (r *Renderer) Reload() error {
	if r == nil || !r.hot {
		return nil
	}
	tpl, err := parseTemplates(r.fsys, r.pattern)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.tpl = tpl
	r.mu.Unlock()
	return nil
}

// UseFS 切换到指定文件系统重新加载模板,并设置 hot 模式标记。
func (r *Renderer) UseFS(fsys fs.FS, hot bool) error {
	if r == nil {
		return nil
	}
	tpl, err := parseTemplates(fsys, r.pattern)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.tpl = tpl
	r.fsys = fsys
	r.hot = hot
	r.mu.Unlock()
	return nil
}

// ReleaseToHotDir 把当前模板写入磁盘目录,并切换到该目录的 hot 模式。
// 这样单二进制启动后也能在后台释放模板文件并启用手动热更新。
func (r *Renderer) ReleaseToHotDir(dir string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hot {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.Wrap(err, "create template dir")
	}
	if err := fs.WalkDir(r.fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(r.fsys, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		return errors.Wrap(err, "write templates to disk")
	}
	fsys := os.DirFS(dir)
	tpl, err := parseTemplates(fsys, r.pattern)
	if err != nil {
		return err
	}
	r.tpl = tpl
	r.fsys = fsys
	r.hot = true
	return nil
}

func parseTemplates(fsys fs.FS, pattern string) (*template.Template, error) {
	funcs := template.FuncMap{
		"postURL":          func(p *model.Post) string { return permalink.Post(p) },
		"pageURL":          func(p *model.Post) string { return permalink.Page(p) },
		"categoryURL":      permalink.Category,
		"tagURL":           permalink.Tag,
		"safeHTML":         func(s string) template.HTML { return template.HTML(s) },
		"escapeHTML":       stdhtml.EscapeString,
		"listHTML":         listHTML,
		"detailHTML":       detailHTML,
		"hasMore":          func(content string) bool { _, m := wxr.SplitMore(content); return m },
		"gravatar":         gravatar,
		"avatarURL":        avatarURL,
		"avatarPreviewURL": avatarPreviewURL,
		"gravatarPrimary":  gravatarPrimary,
		"gravatarFallback": gravatarFallback,
		"fmtDate":          func(t time.Time) string { return t.Format("2006-01-02") },
		"fmtDateTime":      func(t time.Time) string { return t.Format("2006-01-02 15:04") },
		"fmtFileSize":      fmtFileSize,
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

// Template 返回当前缓存的底层模板。
func (r *Renderer) Template() *template.Template {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tpl
}

// Instance 实现 gin/render.HTMLRender。它始终使用当前缓存的模板实例。
func (r *Renderer) Instance(name string, data any) ginrender.Render {
	r.mu.RLock()
	tpl := r.tpl
	r.mu.RUnlock()
	return ginrender.HTML{Template: tpl, Name: name, Data: data}
}

// listHTML 返回列表页应展示的正文 HTML:有 more 标记则只取之前部分。
func listHTML(p *model.Post) template.HTML {
	above, hasMore := wxr.SplitMore(p.Content)
	if hasMore {
		return template.HTML(HighlightCodeBlocks(SanitizeHTML(above)))
	}
	if p.Excerpt != "" {
		return template.HTML(HighlightCodeBlocks(SanitizeHTML(p.Excerpt)))
	}
	return template.HTML(HighlightCodeBlocks(SanitizeHTML(p.Content)))
}

// detailHTML 返回详情页完整正文,把 <!--more--> 替换为锚点。
func detailHTML(p *model.Post) template.HTML {
	return template.HTML(HighlightCodeBlocks(SanitizeHTML(wxr.RenderDetail(p.Content, p.ID))))
}

var fencedCodeRe = regexp.MustCompile(`(?s)<pre><code(?: class="language-([^"]+)")?>(.*?)</code></pre>`)

var htmlSanitizer = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^language-[A-Za-z0-9_+.-]+$`)).OnElements("code")
	return p
}()

// SanitizeHTML 清理用户可编辑/导入的正文 HTML,保留常见富文本但移除脚本与事件属性。
func SanitizeHTML(src string) string {
	return htmlSanitizer.Sanitize(src)
}

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

// fmtFileSize 将字节数格式化为可读字符串。
func fmtFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// gravatar 由邮箱生成 cravatar(国内镜像)头像 URL。
func gravatar(email string) string {
	return avatarURL(email, "")
}

func avatarURL(email, defaultAvatar string) string {
	return "https://cn.cravatar.com/avatar/" + avatarHash(email) + "?s=48&d=" + normalizeDefaultAvatar(defaultAvatar)
}

func avatarPreviewURL(defaultAvatar string) string {
	url := avatarURL("", defaultAvatar)
	if normalizeDefaultAvatar(defaultAvatar) == "cravatar" {
		return url + "&f=y"
	}
	return url
}

func gravatarPrimary(email string) string {
	return "https://gravatar.com/avatar/" + avatarHash(email) + "?s=48"
}

func gravatarFallback(email string) string {
	return avatarURL(email, "")
}

func avatarHash(email string) string {
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

func normalizeDefaultAvatar(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "mp", "blank", "cravatar", "identicon", "wavatar", "monsterid", "retro", "robohash":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "mp"
	}
}
