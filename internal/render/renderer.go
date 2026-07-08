package render

import (
	"context"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/gin-gonic/gin/render"
	ginrender "github.com/gin-gonic/gin/render"

	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/model"
)

// [Renderer] 实现了 gin 的 [render.HTMLRender] 接口
var _ render.HTMLRender = (*Renderer)(nil)

// Renderer 持有模板配置,既支持静态模板,也支持开发期热更新。
// 项目启动时会将 [Renderer] 实例赋值给 gin 引擎的 HTMLRender
//   - c.HTML(code, name, obj) 时, 会通过 [Renderer.Instance] 生成一个 [Render] 实例
//   - 也可以直接获取到 [Render] 后调用 c.Render(code, render)
type Renderer struct {
	mu         sync.RWMutex
	tpl        *template.Template
	fsys       fs.FS
	pattern    string
	hot        bool
	defaultFS  fs.FS // admin/auth 模板（embed 或 hot disk）
	defaultHot bool

	// 默认主题模板（embed），用于 ResetToDefault 时从 embed 加载
	// [initThemeManager] 时设置
	defaultThemeFS fs.FS
	// loadTheme 时设置
	themeDir string

	// 主题预览：单独缓存预览主题的模板，不影响主模板
	// 后台预览时设置 admin.ThemePreview -> tm.LoadPreviewTheme -> [Renderer.LoadPreviewTheme]
	previewTpl       *template.Template
	previewThemeName string

	// 通过 [Renderer.ConfigureTemplateRuntime], [Renderer.SetHookInvokeProvider],
	// [Renderer.SetThemeWidgetsProvider], [Renderer.SetWidgetResolver] 设置
	themeRuntime TemplateRuntime
}

// Instance 实现 gin 的 [render.HTMLRender] 接口。它始终使用当前缓存的模板实例。
func (r *Renderer) Instance(name string, data any) ginrender.Render {
	r.mu.RLock()
	tpl := r.tpl
	r.mu.RUnlock()
	return &themeHTMLRender{tmpl: tpl, name: name, data: data, runtime: &r.themeRuntime}
}

// ========== ========== ========== ========== ========== ========== ==========

// NewHot 从磁盘目录创建一个可热更新的模板渲染器。
// 启动时会先 Parse 一次模板,之后只有显式调用 Reload 才会重新解析。
func NewHot(dir string) (*Renderer, error) {
	fsys := os.DirFS(dir)
	r, err := New(fsys)
	if err != nil {
		return nil, err
	}
	r.hot = true
	r.defaultHot = true
	return r, nil
}

// New 从给定文件系统(通常是 embed.FS 的子目录)加载所有 *.gohtml 模板。
func New(fsys fs.FS) (*Renderer, error) {
	tpl, err := parseTemplates(fsys, pattern)
	if err != nil {
		return nil, err
	}
	return &Renderer{
		tpl:       tpl,
		fsys:      fsys,
		pattern:   pattern,
		defaultFS: fsys,
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

// HasTemplate 检查指定名称的模板是否存在于当前模板集中。
func (r *Renderer) HasTemplate(name string) bool {
	r.mu.RLock()
	tpl := r.tpl
	r.mu.RUnlock()
	return tpl != nil && tpl.Lookup(name) != nil
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

// Template 返回当前缓存的底层模板。
func (r *Renderer) Template() *template.Template {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tpl
}

// Hooks 返回当前 hook 执行器，供非模板渲染路径（如 Feed）使用。
func (r *Renderer) Hooks() hook.Executor {
	return r.themeRuntime.current().Hooks
}

// FilterPostContent 对文章正文 HTML 应用 post.content_html 和 post.footer_html filter 链。
// 供 Feed 等非模板渲染路径使用，确保表情转换、尾部 HTML 等在 RSS 阅读器中生效。
func (r *Renderer) FilterPostContent(ctx context.Context, post *model.Post) template.HTML {
	html := detailHTML(post)
	if h := r.Hooks(); h != nil {
		payload := any(post)
		if view := hook.PostViewOf(post, nil); view != nil {
			payload = *view
		}
		html = htmlFromFilterValue(h.ApplyFilters(ctx, hook.FilterPostContentHTML, string(html), payload))
		html = htmlFromFilterValue(h.ApplyFilters(ctx, hook.FilterPostFooterHTML, string(html), payload))
	}
	return html
}
