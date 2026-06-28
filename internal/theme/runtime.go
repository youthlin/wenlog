package theme

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/plugin"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
)

// RecoveryInfo 记录主题加载失败时的恢复信息。
type RecoveryInfo struct {
	FailedTheme string
	Error       string
}

// BindTemplateFunctions 把主题运行时能力绑定到模板函数。
//
// 主题模板中的函数调用统一走这条链路：
//
//	模板函数 → render.RequestContext → theme.Manager → store/主题声明。
//
// 这样 cmd/server 只负责组装依赖，不再了解 widget/option 的 Setting key 细节。
func (m *Manager) BindTemplateFunctions() {
	if m == nil || m.renderer == nil {
		return
	}
	m.renderer.SetThemeWidgetsProvider(func(ctx *render.RequestContext, area string) []render.WidgetInfo {
		t := m.renderTheme(ctx)
		if t == nil {
			return nil
		}
		config := m.renderSetting(ctx, "widget_"+area)
		return ResolveWidgetsWithDecls(config, t, area, m.WidgetDecls(renderContext(ctx), t, area))
	})

	m.renderer.SetOptionProvider(func(ctx *render.RequestContext, optionID string) string {
		t := m.renderTheme(ctx)
		if t == nil {
			return ""
		}
		return GetOptionByID(func(key string) (string, error) {
			return m.renderSetting(ctx, key), nil
		}, t.Name, t.Options, optionID)
	})
}

// WidgetDecls 返回当前主题可用组件声明，并追加当前启用插件声明的小组件。
func (m *Manager) WidgetDecls(ctx context.Context, t *Theme, area string) []WidgetDecl {
	if m == nil {
		return WidgetDeclsWithBuiltins(t)
	}
	return WidgetDeclsWithPlugins(t, m.pluginWidgetDecls(ctx))
}

func (m *Manager) pluginWidgetDecls(ctx context.Context) []plugin.WidgetDecl {
	if m == nil {
		return nil
	}
	m.runtimeMu.Lock()
	provider := m.pluginWidgets
	m.runtimeMu.Unlock()
	if provider == nil {
		return nil
	}
	return provider(ctx)
}

func (m *Manager) hookRegistry() *plugin.Registry {
	if m == nil {
		return nil
	}
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	return m.hooks
}

func renderContext(ctx *render.RequestContext) context.Context {
	if ctx != nil && ctx.Context != nil {
		return ctx.Context
	}
	return context.Background()
}

func (m *Manager) renderSetting(ctx *render.RequestContext, key string) string {
	if ctx != nil {
		if loader, ok := ctx.ThemeLoader.(*store.DataLoader); ok && loader != nil {
			return loader.GetSetting(key)
		}
	}
	if m == nil || m.store == nil {
		return ""
	}
	value, _ := m.store.GetSetting(context.Background(), key)
	return value
}

func (m *Manager) renderTheme(ctx *render.RequestContext) *Theme {
	if ctx != nil {
		if t, ok := ctx.Theme.(*Theme); ok && t != nil {
			return t
		}
	}
	if m == nil {
		return nil
	}
	return m.Current(context.Background())
}

// LoadTheme 加载指定主题：模板 + functions.go。
// 任一步骤失败则自动回退到默认主题。
// name 为空时加载当前激活的主题。
// 返回 nil 表示加载成功；返回 error 表示已回退到默认主题。
func (m *Manager) LoadTheme(ctx context.Context, name string) error {
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()

	if name == "" {
		var err error
		name, err = m.store.GetSetting(ctx, settingCurrentTheme)
		if err != nil || name == "" {
			name = defaultThemeName
		}
	}

	m.mu.RLock()
	t := m.themes[name]
	m.mu.RUnlock()
	if t == nil {
		err := errors.Errorf("主题[%s]未找到", name)
		return m.fallbackToDefault(ctx, name, err)
	}

	// 1. 加载模板
	templatesDir := t.TemplatesDir()
	if m.renderer != nil {
		if err := m.renderer.LoadThemeTemplates(templatesDir); err != nil {
			err = errors.Wrap(err, "加载主题模板失败")
			return m.fallbackToDefault(ctx, name, err)
		}
	}

	// 2. 编译 functions.go（如果存在）
	api := NewAPI(nil)             // loader 在模板渲染时按请求注入
	api.SetThemeOptions(t.Options) // 设置选项默认值，供 GetOption 回退
	api.SetHookRegistry(m.hooks, plugin.Source{Type: plugin.SourceTheme, ID: t.Name})
	script, err := CompileFunctions(t.Dir, api, m.log)
	if err != nil {
		err = errors.Wrap(err, "编译主题functions.go失败")
		return m.fallbackToDefault(ctx, name, err)
	}

	// 3. 成功：更新状态
	m.mu.Lock()
	m.currentScript = script
	m.currentAPI = api
	m.recoveryInfo = nil
	m.mu.Unlock()

	// 注册 hookInvoke 模板函数
	if m.renderer != nil {
		m.renderer.SetHookInvokeProvider(hookInvokeFunc(script, api))
	}

	if m.log != nil {
		m.log.Info("主题加载成功",
			"name", name,
			"has_functions", script != nil,
		)
	}

	return nil
}

// fallbackToDefault 回退到默认主题。
func (m *Manager) fallbackToDefault(ctx context.Context, failedName string, err error) error {
	if m.log != nil {
		m.log.Error("theme load failed, falling back to default",
			"failed_theme", failedName,
			"error", err,
		)
	}

	m.mu.Lock()
	m.recoveryInfo = &RecoveryInfo{
		FailedTheme: failedName,
		Error:       err.Error(),
	}
	m.currentScript = nil
	m.currentAPI = nil
	m.mu.Unlock()

	if m.renderer != nil {
		m.renderer.SetHookInvokeProvider(nil)
	}

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
