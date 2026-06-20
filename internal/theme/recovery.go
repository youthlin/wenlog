package theme

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
)

// ThemeRenderer 是 Manager 需要的模板渲染器接口。
type ThemeRenderer interface {
	LoadTheme(themeDir string) error
	ResetToDefault() error
	LoadPreviewTheme(themeDir, themeName string) error
	ClearPreviewTheme()
}

// RecoveryInfo 记录主题加载失败时的恢复信息。
type RecoveryInfo struct {
	FailedTheme string
	Error       string
}

// LoadTheme 加载指定主题：模板 + functions.go。
// 任一步骤失败则自动回退到默认主题。
// name 为空时加载当前激活的主题。
// 返回 nil 表示加载成功；返回 error 表示已回退到默认主题。
func (m *Manager) LoadTheme(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if name == "" {
		var err error
		name, err = m.store.GetSetting(ctx, settingCurrentTheme)
		if err != nil || name == "" {
			name = defaultThemeName
		}
	}

	t := m.themes[name]
	if t == nil {
		return m.fallbackToDefaultLocked(ctx, name, fmt.Errorf("theme %q not found", name))
	}

	// 1. 加载模板
	templatesDir := t.TemplatesDir()
	if m.renderer != nil {
		if err := m.renderer.LoadTheme(templatesDir); err != nil {
			return m.fallbackToDefaultLocked(ctx, name, errors.Wrap(err, "load theme templates"))
		}
	}

	// 2. 编译 functions.go（如果存在）
	api := NewAPI(nil) // loader 在请求时注入
	script, err := CompileFunctions(t.Dir, api, m.log)
	if err != nil {
		return m.fallbackToDefaultLocked(ctx, name, errors.Wrap(err, "compile functions.go"))
	}

	// 3. 成功：更新状态
	m.currentScript = script
	m.currentAPI = api
	m.recoveryInfo = nil

	// 注册 themeData 模板函数
	if script != nil {
		render.SetThemeDataProvider(themeDataFunc(script))
	} else {
		render.SetThemeDataProvider(nil)
	}

	if m.log != nil {
		m.log.Info("theme loaded successfully",
			"name", name,
			"has_functions", script != nil,
		)
	}

	return nil
}

// fallbackToDefaultLocked 回退到默认主题（调用方必须持有 m.mu 写锁）。
func (m *Manager) fallbackToDefaultLocked(ctx context.Context, failedName string, err error) error {
	if m.log != nil {
		m.log.Error("theme load failed, falling back to default",
			"failed_theme", failedName,
			"error", err,
		)
	}

	// 记录恢复信息（供后台展示警告）
	m.recoveryInfo = &RecoveryInfo{
		FailedTheme: failedName,
		Error:       err.Error(),
	}

	// 清空自定义 DataProvider
	m.currentScript = nil
	m.currentAPI = nil
	render.SetThemeDataProvider(nil)

	// 回退模板到默认主题
	if m.renderer != nil {
		if rerr := m.renderer.ResetToDefault(); rerr != nil && m.log != nil {
			m.log.Error("reset to default theme failed", "error", rerr)
		}
	}

	// 持久化 current_theme 为 default
	if serr := m.store.SetSetting(ctx, settingCurrentTheme, defaultThemeName); serr != nil && m.log != nil {
		m.log.Error("persist default theme fallback failed", "error", serr)
	}

	return err
}

// RecoveryInfo 返回最近一次主题加载失败的恢复信息，nil 表示一切正常。
func (m *Manager) GetRecoveryInfo() *RecoveryInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.recoveryInfo
}

// ClearRecoveryInfo 清除恢复信息（管理员确认后调用）。
func (m *Manager) ClearRecoveryInfo() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recoveryInfo = nil
}

// CurrentScript 返回当前激活的 functions.go 脚本，nil 表示无脚本。
func (m *Manager) CurrentScript() *FunctionsScript {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentScript
}

// SetLoaderForRequest 设置当前请求的 DataLoader（每次请求前由 handler 调用）。
func (m *Manager) SetLoaderForRequest(loader *store.DataLoader) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.currentAPI != nil {
		m.currentAPI.SetLoader(loader)
	}
}

// SetRenderer 设置模板渲染器（由 cmd/server 初始化时调用）。
func (m *Manager) SetRenderer(r ThemeRenderer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.renderer = r
}

// SetLogger 设置日志记录器。
func (m *Manager) SetLogger(log *slog.Logger) {
	m.log = log
}

// ReloadCurrentTheme 重新加载当前激活的主题（文件编辑器保存后调用）。
func (m *Manager) ReloadCurrentTheme(ctx context.Context) error {
	name, err := m.store.GetSetting(ctx, settingCurrentTheme)
	if err != nil || name == "" {
		name = defaultThemeName
	}
	return m.LoadTheme(ctx, name)
}

// LoadPreviewTheme 加载预览主题的模板到 Renderer 的独立缓存，不影响主模板。
func (m *Manager) LoadPreviewTheme(name string) error {
	m.mu.RLock()
	t := m.themes[name]
	renderer := m.renderer
	m.mu.RUnlock()

	if t == nil {
		return fmt.Errorf("theme %q not found", name)
	}
	if renderer == nil {
		return nil
	}
	return renderer.LoadPreviewTheme(t.TemplatesDir(), name)
}

// ClearPreviewTheme 清除 Renderer 中的预览主题模板缓存。
func (m *Manager) ClearPreviewTheme() {
	if m.renderer != nil {
		m.renderer.ClearPreviewTheme()
	}
}

// ThemeDir 返回指定主题的根目录路径。
func (m *Manager) ThemeDir(name string) string {
	t := m.Get(name)
	if t == nil {
		return ""
	}
	return t.Dir
}

// ThemeFilePath 校验并返回主题文件的安全路径。
// 防止路径穿越攻击。
func (m *Manager) ThemeFilePath(name, relPath string) (string, error) {
	t := m.Get(name)
	if t == nil {
		return "", fmt.Errorf("theme %q not found", name)
	}
	// 清理路径
	clean := filepath.Clean(relPath)
	// 拼接并再次清理
	fullPath := filepath.Clean(filepath.Join(t.Dir, clean))
	// 确保结果在主题目录内
	if !strings.HasPrefix(fullPath, t.Dir) {
		return "", fmt.Errorf("path traversal detected: %s", relPath)
	}
	return fullPath, nil
}

// ListThemeFiles 列出主题目录下的所有可编辑文件。
func (m *Manager) ListThemeFiles(name string) ([]ThemeFile, error) {
	t := m.Get(name)
	if t == nil {
		return nil, fmt.Errorf("theme %q not found", name)
	}
	var files []ThemeFile
	err := filepath.WalkDir(t.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(t.Dir, path)
		if err != nil {
			return err
		}
		if !isEditableThemeFile(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, ThemeFile{
			Path: rel,
			Size: info.Size(),
		})
		return nil
	})
	return files, err
}

// ThemeFile 表示主题中的一个可编辑文件。
type ThemeFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// isEditableThemeFile 检查文件是否可在线编辑。
func isEditableThemeFile(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".gohtml", ".html", ".css", ".js", ".yaml", ".yml", ".po", ".mo":
		return true
	case ".go", ".goyaegi":
		// 只允许 functions.go / functions.goyaegi
		base := filepath.Base(path)
		return base == "functions.go" || base == "functions.goyaegi"
	default:
		return false
	}
}

// mu 保护并发访问。
// 注意：Manager 已有 themes map，这里添加额外的并发保护字段。
// 为了不破坏现有结构，在 recovery.go 中通过嵌入方式扩展。
// 实际上直接在 Manager 结构体中添加字段更简单。
// 这里使用 sync.RWMutex 保护新增字段。
var _ = sync.RWMutex{} // 确保 import 被使用
