package theme

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/render"
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
	api := NewAPI(nil)             // loader 在模板渲染时按请求注入
	api.SetThemeOptions(t.Options) // 设置选项默认值，供 GetOption 回退
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
		render.SetThemeDataProvider(themeDataFunc(script, api))
	} else {
		render.SetThemeDataProvider(nil)
	}

	if m.log != nil {
		m.log.Info("主题加载成功",
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
