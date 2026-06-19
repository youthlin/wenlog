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
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/cockroachdb/errors"
	ginrender "github.com/gin-gonic/gin/render"
	"github.com/gomarkdown/markdown"
	mhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/microcosm-cc/bluemonday"
	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/util"
	"github.com/youthlin/blog/internal/wxr"
)

// Renderer 持有模板配置,既支持静态模板,也支持开发期热更新。
type Renderer struct {
	mu             sync.RWMutex
	tpl            *template.Template
	fsys           fs.FS
	pattern        string
	hot            bool
	defaultFS      fs.FS // admin/auth 模板（embed 或 hot disk）
	defaultHot     bool
	defaultThemeFS fs.FS // 默认主题模板（embed），用于 ResetToDefault 时从 embed 加载
	themeDir       string
}

const pattern = "*.gohtml"

// New 从给定文件系统(通常是 embed.FS 的子目录)加载所有 *.gohtml 模板。
func New(fsys fs.FS) (*Renderer, error) {
	tpl, err := parseTemplates(fsys, pattern)
	if err != nil {
		return nil, err
	}
	return &Renderer{
		tpl:        tpl,
		fsys:       fsys,
		pattern:    pattern,
		defaultFS:  fsys,
		defaultHot: false,
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
		tpl:        tpl,
		fsys:       fsys,
		pattern:    pattern,
		hot:        true,
		defaultFS:  fsys,
		defaultHot: true,
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

// LoadTheme 加载主题模板目录。先解析主题模板，再补充默认主题模板中缺失的，
// 最后补充 admin/auth 模板中缺失的。主题模板覆盖同名的默认/后台模板。
func (r *Renderer) LoadTheme(themeDir string) error {
	if r == nil {
		return nil
	}
	themeFS := os.DirFS(themeDir)
	// 解析主题模板（主题必须自包含，不再从默认主题 fallback）
	themeTpl, err := parseTemplates(themeFS, r.pattern)
	if err != nil {
		return err
	}
	// 补充 admin/auth 模板中缺失的（基础设施，非主题范畴）
	r.fallbackFromDefaultFS(themeTpl)
	r.mu.Lock()
	r.tpl = themeTpl
	r.fsys = themeFS
	r.hot = false
	r.themeDir = themeDir
	r.mu.Unlock()
	return nil
}

// TemplateHierarchy 定义页面类型到模板文件名的 fallback 链。
// 主题只需提供链中任一模板即可，系统按顺序查找第一个存在的。
var TemplateHierarchy = map[string][]string{
	"index":   {"index.gohtml"},
	"post":    {"post.gohtml", "index.gohtml"},
	"page":    {"page.gohtml", "post.gohtml", "index.gohtml"},
	"search":  {"search.gohtml", "list.gohtml", "index.gohtml"},
	"list":    {"list.gohtml", "index.gohtml"},
	"archive": {"archive.gohtml", "list.gohtml", "index.gohtml"},
	"error":   {"error.gohtml"},
}

// ResolveTemplate 根据页面类型查找主题中第一个存在的模板。
// 如果链中所有模板都不存在，返回链中第一个（让 Go template 报错）。
func (r *Renderer) ResolveTemplate(pageType string) string {
	r.mu.RLock()
	tpl := r.tpl
	r.mu.RUnlock()

	chain, ok := TemplateHierarchy[pageType]
	if !ok {
		return pageType + ".gohtml"
	}
	for _, name := range chain {
		if tpl != nil && tpl.Lookup(name) != nil {
			return name
		}
	}
	return chain[0]
}

// HasTemplate 检查指定名称的模板是否存在于当前模板集中。
func (r *Renderer) HasTemplate(name string) bool {
	r.mu.RLock()
	tpl := r.tpl
	r.mu.RUnlock()
	return tpl != nil && tpl.Lookup(name) != nil
}

// ResolveFragment 将 fragment 名（如 "comments"）转为模板名（如 "fragment_comments.gohtml"），
// 并检查该模板是否存在。返回模板名和是否找到。
// 主题通过定义 fragment_<name>.gohtml 来支持对应 fragment 的局部渲染。
func (r *Renderer) ResolveFragment(name string) (string, bool) {
	tplName := "fragment_" + name + ".gohtml"
	if !r.HasTemplate(tplName) {
		return "", false
	}
	return tplName, true
}

// fallbackFromDefaultFS 从 admin/auth 模板补充缺失的模板。
func (r *Renderer) fallbackFromDefaultFS(themeTpl *template.Template) {
	if r.defaultFS == nil || !hasMatchingFiles(r.defaultFS, r.pattern) {
		return
	}
	defaultTpl, err := parseTemplates(r.defaultFS, r.pattern)
	if err != nil {
		return
	}
	for _, t := range defaultTpl.Templates() {
		if themeTpl.Lookup(t.Name()) == nil {
			_, _ = themeTpl.AddParseTree(t.Name(), t.Tree)
		}
	}
}

// ResetToDefault 重置为默认主题模板 + admin/auth 回退。
// 优先从磁盘 themes/default/templates/ 加载，不存在时回退 embed。
func (r *Renderer) ResetToDefault() error {
	if r == nil {
		return nil
	}
	// 先尝试磁盘默认主题
	themeDir := filepath.Join("themes", "default", "templates")
	if _, err := os.Stat(themeDir); err == nil {
		return r.LoadTheme(themeDir)
	}
	// 磁盘不存在时从 embed 加载默认主题
	if r.defaultThemeFS != nil {
		return r.loadThemeFS(r.defaultThemeFS)
	}
	// 兜底：只用 admin/auth 模板
	if r.defaultFS == nil {
		return nil
	}
	tpl, err := parseTemplates(r.defaultFS, r.pattern)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.tpl = tpl
	r.fsys = r.defaultFS
	r.hot = r.defaultHot
	r.themeDir = ""
	r.mu.Unlock()
	return nil
}

// ThemeDir 返回当前加载的主题模板目录，空字符串表示使用默认模板。
func (r *Renderer) ThemeDir() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.themeDir
}

// SetDefaultThemeFS 设置默认主题的 embed 文件系统，用于 ResetToDefault 时从 embed 加载。
func (r *Renderer) SetDefaultThemeFS(fsys fs.FS) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.defaultThemeFS = fsys
	r.mu.Unlock()
}

// themeDataProvider 是 themeData 模板函数的实际实现，由 theme.Manager 注入。
var themeDataProvider func(name string, args ...any) any

// SetThemeDataProvider 设置 themeData 模板函数的实现（由 theme.Manager 注入）。
func SetThemeDataProvider(fn func(name string, args ...any) any) {
	themeDataProvider = fn
}

func themeData(name string, args ...any) any {
	if themeDataProvider == nil {
		return nil
	}
	return themeDataProvider(name, args...)
}

// loadThemeFS 从 fs.FS 加载主题模板（如 embed 默认主题），再补充 admin/auth 回退。
func (r *Renderer) loadThemeFS(themeFS fs.FS) error {
	themeTpl, err := parseTemplates(themeFS, r.pattern)
	if err != nil {
		return err
	}
	// 补充 admin/auth 模板
	if r.defaultFS != nil && hasMatchingFiles(r.defaultFS, r.pattern) {
		defaultTpl, err := parseTemplates(r.defaultFS, r.pattern)
		if err != nil {
			return err
		}
		for _, t := range defaultTpl.Templates() {
			if themeTpl.Lookup(t.Name()) == nil {
				if _, err := themeTpl.AddParseTree(t.Name(), t.Tree); err != nil {
					return err
				}
			}
		}
	}
	r.mu.Lock()
	r.tpl = themeTpl
	r.fsys = themeFS
	r.hot = false
	r.themeDir = ""
	r.mu.Unlock()
	return nil
}

// hasMatchingFiles 检查 fsys 中是否存在匹配 pattern 的文件。
func hasMatchingFiles(fsys fs.FS, pattern string) bool {
	matches, err := fs.Glob(fsys, pattern)
	return err == nil && len(matches) > 0
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
		"postURL":          postURL,
		"categoryURL":      permalink.Category,
		"tagURL":           permalink.Tag,
		"safeHTML":         func(s string) template.HTML { return template.HTML(s) },
		"escapeHTML":       stdhtml.EscapeString,
		"postExcerptHTML":  postExcerptHTML,
		"detailHTML":       detailHTML,
		"hasMore":          func(content string) bool { _, m := wxr.SplitMore(content); return m },
		"avatarURL":        avatarURL,
		"defaultAvatarURL": defaultAvatarURL,
		"fmtDate":          func(t time.Time) string { return t.Format("2006-01-02") },
		"fmtDateTime":      func(t time.Time) string { return t.Format("2006-01-02 15:04") },
		"fmtFileSize":      fmtFileSize,
		"year":             func(t time.Time) int { return t.Year() },
		"add":              func(a, b int) int { return a + b },
		"sub":              func(a, b int) int { return a - b },
		"seq":              seq,
		"themeData":        themeData,
	}
	tpl, err := template.New("").Funcs(funcs).ParseFS(fsys, pattern)
	if err != nil {
		return nil, errors.Wrap(err, "parse templates")
	}
	return tpl, nil
}

func postURL(p any) string {
	switch v := p.(type) {
	case *model.Post:
		return permalink.Post(v)
	case model.Post:
		return permalink.Post(&v)
	default:
		// 处理 yaegi 返回的 theme.PostView（通过反射提取 ID/Title/Slug/PostType/PublishedAt/ModifiedAt）
		rv := reflect.ValueOf(p)
		if rv.Kind() == reflect.Struct {
			id := reflectGetUint(rv, "ID")
			title := reflectGetString(rv, "Title")
			slug := reflectGetString(rv, "Slug")
			postType := reflectGetString(rv, "PostType")
			publishedAt, _ := reflectGetTime(rv, "PublishedAt")
			modifiedAt, _ := reflectGetTime(rv, "ModifiedAt")
			if id > 0 {
				mp := &model.Post{
					ID:          id,
					Title:       title,
					Slug:        slug,
					PostType:    postType,
					Status:      model.StatusPublished,
					PublishedAt: publishedAt,
					ModifiedAt:  modifiedAt,
				}
				if postType == model.PostTypePage {
					return permalink.Page(mp)
				}
				return permalink.Post(mp)
			}
		}
		return ""
	}
}

func reflectGetUint(rv reflect.Value, field string) uint {
	f := rv.FieldByName(field)
	if f.IsValid() {
		switch f.Kind() {
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return uint(f.Uint())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			v := f.Int()
			if v >= 0 {
				return uint(v)
			}
		}
	}
	return 0
}

func reflectGetString(rv reflect.Value, field string) string {
	f := rv.FieldByName(field)
	if f.IsValid() && f.Kind() == reflect.String {
		return f.String()
	}
	return ""
}

func reflectGetTime(rv reflect.Value, field string) (time.Time, bool) {
	f := rv.FieldByName(field)
	if f.IsValid() && f.Type() == reflect.TypeOf(time.Time{}) {
		return f.Interface().(time.Time), true
	}
	return time.Time{}, false
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

// postExcerptHTML 返回列表页应展示的文章摘要 HTML: 有 more 标记则只取之前部分。
func postExcerptHTML(p *model.Post) template.HTML {
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

// avatarURL 由邮箱生成 cravatar(国内镜像)头像 URL。
func avatarURL(email, defaultAvatar string) string {
	return "https://cn.cravatar.com/avatar/" + avatarHash(email) + "?s=" + strconv.Itoa(consts.AvatarSizeSmall) + "&d=" + util.NormalizeDefaultAvatar(defaultAvatar)
}

// defaultAvatarURL 获取强制展示指定默认头像的链接。
func defaultAvatarURL(defaultAvatar string) string {
	url := avatarURL("", defaultAvatar)
	return url + "&f=y"
}

func avatarHash(email string) string {
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

// RenderMarkdown 把 Markdown 渲染为 HTML,消毒后复用统一的代码块高亮逻辑。
func RenderMarkdown(md string) string {
	p := parser.NewWithExtensions(parser.CommonExtensions | parser.AutoHeadingIDs)
	doc := p.Parse([]byte(md))
	renderer := mhtml.NewRenderer(mhtml.RendererOptions{Flags: mhtml.CommonFlags})
	out := string(markdown.Render(doc, renderer))
	return HighlightCodeBlocks(SanitizeHTML(out))
}
