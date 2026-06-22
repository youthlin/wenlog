package render

import (
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/gin-gonic/gin/render"
	ginrender "github.com/gin-gonic/gin/render"
	"github.com/youthlin/blog/web"
)

// [Renderer] 实现了 gin 的 [render.HTMLRender] 接口
var _ render.HTMLRender = (*Renderer)(nil)

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

	// 主题预览：单独缓存预览主题的模板，不影响主模板
	previewTpl       *template.Template
	previewThemeName string
}

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

// Instance 实现 gin 的 [render.HTMLRender] 接口。它始终使用当前缓存的模板实例。
func (r *Renderer) Instance(name string, data any) ginrender.Render {
	r.mu.RLock()
	tpl := r.tpl
	r.mu.RUnlock()
	return &widgetHTMLRender{tmpl: tpl, name: name, data: data}
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

// LoadTheme 加载主题模板目录。先解析主题模板，再补充内置组件模板，
// 再补充主题自定义组件模板（覆盖内置），最后补充 admin/auth 模板中缺失的。
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
	// 补充主题自定义组件模板（themes/<name>/widgets/），优先于内置
	widgetsDir := filepath.Join(filepath.Dir(themeDir), "widgets")
	if _, err := os.Stat(widgetsDir); err == nil {
		widgetsFS := os.DirFS(widgetsDir)
		r.fallbackWidgets(themeTpl, widgetsFS)
	}
	// 补充内置组件模板（web/widgets/），仅补充主题未提供的
	r.fallbackWidgets(themeTpl, nil)
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

// fallbackWidgets 补充组件模板。如果 widgetsFS 为 nil，从 embed web.Widgets 加载内置组件；
// 否则从指定 FS 加载（主题自定义组件）。仅补充 themeTpl 中不存在的模板。
func (r *Renderer) fallbackWidgets(themeTpl *template.Template, widgetsFS fs.FS) {
	if widgetsFS == nil {
		// 从 embed 加载内置组件（web.Widgets embed 根是 widgets/ 子目录）
		var err error
		widgetsFS, err = fs.Sub(web.Widgets, "widgets")
		if err != nil {
			return
		}
	}
	if !hasMatchingFiles(widgetsFS, r.pattern) {
		return
	}
	widgetsTpl, err := parseTemplates(widgetsFS, r.pattern)
	if err != nil {
		return
	}
	for _, t := range widgetsTpl.Templates() {
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

// loadThemeFS 从 fs.FS 加载主题模板（如 embed 默认主题），再补充 admin/auth 回退。
func (r *Renderer) loadThemeFS(themeFS fs.FS) error {
	themeTpl, err := parseTemplates(themeFS, r.pattern)
	if err != nil {
		return err
	}
	// 补充内置组件模板
	r.fallbackWidgets(themeTpl, nil)
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

// Template 返回当前缓存的底层模板。
func (r *Renderer) Template() *template.Template {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tpl
}

// LoadPreviewTheme 加载预览主题的模板到独立缓存，不影响主模板。
func (r *Renderer) LoadPreviewTheme(themeDir, themeName string) error {
	if r == nil {
		return nil
	}
	themeFS := os.DirFS(themeDir)
	themeTpl, err := parseTemplates(themeFS, r.pattern)
	if err != nil {
		return err
	}
	// 补充主题自定义组件模板
	widgetsDir := filepath.Join(filepath.Dir(themeDir), "widgets")
	if _, err := os.Stat(widgetsDir); err == nil {
		widgetsFS := os.DirFS(widgetsDir)
		r.fallbackWidgets(themeTpl, widgetsFS)
	}
	// 补充内置组件模板
	r.fallbackWidgets(themeTpl, nil)
	r.fallbackFromDefaultFS(themeTpl)
	r.mu.Lock()
	r.previewTpl = themeTpl
	r.previewThemeName = themeName
	r.mu.Unlock()
	return nil
}

// ClearPreviewTheme 清除预览主题模板缓存。
func (r *Renderer) ClearPreviewTheme() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.previewTpl = nil
	r.previewThemeName = ""
	r.mu.Unlock()
}
