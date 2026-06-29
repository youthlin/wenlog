package plugin

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/i18n"
)

const settingEnabledPlugins = "enabled_plugins"

const (
	LifecycleActivate   = "Activate"
	LifecycleDeactivate = "Deactivate"
	LifecycleUninstall  = "Uninstall"
)

// Manager 管理插件扫描、启用列表和共享 Hook Registry。
type Manager struct {
	pluginsDir string
	store      hook.SettingStore
	plugins    map[string]*Plugin
	hooks      *Registry
	scripts    map[string]*FunctionsScript
	loadErrors map[string]string
	log        *slog.Logger
	mu         sync.RWMutex
}

// NewManager 创建插件管理器。pluginsDir 是插件存放目录（如 "plugins"）。
func NewManager(pluginsDir string, store hook.SettingStore) (*Manager, error) {
	m := &Manager{
		pluginsDir: pluginsDir,
		store:      store,
		plugins:    make(map[string]*Plugin),
		hooks:      NewRegistry(),
		scripts:    make(map[string]*FunctionsScript),
		loadErrors: make(map[string]string),
		log:        slog.Default().With("component", "plugin-manager"),
	}
	m.hooks.SetLogger(m.log)
	if err := m.scan(); err != nil {
		return nil, err
	}
	return m, nil
}

// Hooks 返回插件系统共享的 Hook Registry。
func (m *Manager) Hooks() *Registry {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hooks
}

// LoadEnabledFunctions 重新创建 Hook Registry，并按启用顺序编译执行插件 functions。
// 调用方可在启动时或插件启停后调用它；失败的插件会返回错误，避免半加载状态被静默忽略。
func (m *Manager) LoadEnabledFunctions(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	hooks := NewRegistry()
	hooks.SetLogger(m.log)
	scripts := make(map[string]*FunctionsScript)
	loadErrors := make(map[string]string)
	ids := m.enabledIDsLocked(ctx)
	for _, id := range ids {
		p := m.plugins[id]
		if p == nil {
			continue
		}
		script, err := CompileFunctions(p, hooks, m.log)
		if err != nil {
			loadErrors[id] = err.Error()
			m.loadErrors = loadErrors
			return errors.Wrapf(err, "加载插件[%s]运行时失败", id)
		}
		if script != nil {
			scripts[id] = script
		}
	}
	m.hooks = hooks
	m.scripts = scripts
	m.loadErrors = loadErrors
	return nil
}

// EnabledWidgetDecls 返回当前所有已启用插件声明的小组件。
func (m *Manager) EnabledWidgetDecls(ctx context.Context) []WidgetDecl {
	if m == nil {
		return nil
	}
	ids := m.EnabledIDs(ctx)
	result := make([]WidgetDecl, 0)
	for _, id := range ids {
		p := m.Get(id)
		if p == nil || len(p.Widgets) == 0 {
			continue
		}
		for _, w := range p.Widgets {
			if w.ID == "" {
				continue
			}
			w.Source = "plugin"
			w.PluginID = p.ID
			result = append(result, w)
		}
	}
	return result
}

// scan 扫描 pluginsDir 下所有子目录，加载 plugin.yaml。
func (m *Manager) scan() error {
	entries, err := os.ReadDir(m.pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrap(err, "读取插件目录失败")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(m.pluginsDir, entry.Name())
		p, err := LoadPlugin(dir)
		if err != nil {
			m.log.Warn("无效插件",
				slog.String("dir", dir),
				slog.Any("err", err),
			)
			continue
		}
		m.plugins[p.ID] = p
		_ = p.LoadTranslations()
	}
	return nil
}

// LoadTranslations 加载所有已安装插件自带的翻译文件。
func (m *Manager) LoadTranslations() error {
	if m == nil {
		return nil
	}
	return hook.LoadTranslations(m.List())
}

// RebuildTranslations 重新构建应用域和所有插件域的翻译映射。
func (m *Manager) RebuildTranslations() error {
	if m == nil {
		return nil
	}
	return i18n.RebuildDomains(m.LoadTranslations)
}

// List 返回所有已安装插件列表，按 ID 排序。
func (m *Manager) List() []*Plugin {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.plugins))
	for id := range m.plugins {
		names = append(names, id)
	}
	sort.Strings(names)
	result := make([]*Plugin, 0, len(names))
	for _, id := range names {
		result = append(result, m.plugins[id])
	}
	return result
}

// Get 按 ID 获取插件，不存在返回 nil。
func (m *Manager) Get(id string) *Plugin {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.plugins[id]
}

// LoadErrors 返回最近一次插件运行时加载错误快照，key 为插件 ID。
func (m *Manager) LoadErrors() map[string]string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]string, len(m.loadErrors))
	for id, err := range m.loadErrors {
		result[id] = err
	}
	return result
}

// CallLifecycle 调用插件 functions 中可选的生命周期函数。
// 启用中的插件优先复用已编译脚本；未启用插件则临时编译一份，只执行生命周期，不污染当前 Hook Registry。
func (m *Manager) CallLifecycle(ctx context.Context, id, name string) error {
	if m == nil || id == "" || name == "" {
		return nil
	}
	p := m.Get(id)
	if p == nil {
		return errors.Errorf("plugin %q not found", id)
	}
	m.mu.RLock()
	loaded := m.scripts[id]
	m.mu.RUnlock()
	if loaded != nil {
		return loaded.CallLifecycle(ctx, name)
	}

	hooks := NewRegistry()
	hooks.SetLogger(m.log)
	tmp, err := CompileFunctions(p, hooks, m.log)
	if err != nil {
		return errors.Wrapf(err, "加载插件[%s]生命周期运行时失败", id)
	}
	if tmp == nil {
		return nil
	}
	return tmp.CallLifecycle(ctx, name)
}

// Uninstall 执行插件卸载生命周期并从启用列表移除。插件目录本身不在这里删除。
func (m *Manager) Uninstall(ctx context.Context, id string) error {
	if m == nil || id == "" {
		return nil
	}
	if enabledIDsContain(m.EnabledIDs(ctx), id) {
		if err := m.CallLifecycle(ctx, id, LifecycleDeactivate); err != nil {
			return err
		}
	}
	if err := m.CallLifecycle(ctx, id, LifecycleUninstall); err != nil {
		return err
	}
	return m.Disable(ctx, id)
}

// HasAssets 检查插件是否包含静态资源目录。
func (p *Plugin) HasAssets() bool {
	if p == nil {
		return false
	}
	info, err := os.Stat(p.AssetsDir())
	return err == nil && info.IsDir()
}

// EnabledIDs 返回当前持久化启用的插件 ID 列表。
func (m *Manager) EnabledIDs(ctx context.Context) []string {
	if m == nil || m.store == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabledIDsLocked(ctx)
}

func (m *Manager) enabledIDsLocked(ctx context.Context) []string {
	raw, err := m.store.GetSetting(ctx, settingEnabledPlugins)
	if err != nil || raw == "" {
		return m.defaultEnabledIDsLocked()
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}
	installed := make(map[string]bool, len(m.plugins))
	for id := range m.plugins {
		installed[id] = true
	}
	return filterInstalledIDs(ids, installed)
}

func (m *Manager) defaultEnabledIDsLocked() []string {
	ids := make([]string, 0, len(m.plugins))
	for id, p := range m.plugins {
		if p != nil && p.DefaultEnabled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// Enabled 返回当前启用且已安装的插件列表，顺序与设置中保存的一致。
func (m *Manager) Enabled(ctx context.Context) []*Plugin {
	ids := m.EnabledIDs(ctx)
	if len(ids) == 0 {
		return nil
	}
	result := make([]*Plugin, 0, len(ids))
	for _, id := range ids {
		if p := m.Get(id); p != nil {
			result = append(result, p)
		}
	}
	return result
}

// Enable 启用指定插件并持久化。
func (m *Manager) Enable(ctx context.Context, id string) error {
	if m == nil || m.store == nil {
		return nil
	}
	if m.Get(id) == nil {
		return errors.Errorf("plugin %q not found", id)
	}
	ids := m.EnabledIDs(ctx)
	for _, current := range ids {
		if current == id {
			return nil
		}
	}
	ids = append(ids, id)
	return m.saveEnabledIDs(ctx, ids)
}

// Disable 停用指定插件并持久化。
func (m *Manager) Disable(ctx context.Context, id string) error {
	if m == nil || m.store == nil {
		return nil
	}
	ids := m.EnabledIDs(ctx)
	out := ids[:0]
	for _, current := range ids {
		if current != id {
			out = append(out, current)
		}
	}
	return m.saveEnabledIDs(ctx, out)
}

func (m *Manager) saveEnabledIDs(ctx context.Context, ids []string) error {
	data, err := json.Marshal(ids)
	if err != nil {
		return errors.Wrap(err, "序列化启用插件列表失败")
	}
	return m.store.SetSetting(ctx, settingEnabledPlugins, string(data))
}

func (m *Manager) installedSet() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	installed := make(map[string]bool, len(m.plugins))
	for id := range m.plugins {
		installed[id] = true
	}
	return installed
}

func filterInstalledIDs(ids []string, installed map[string]bool) []string {
	if len(ids) == 0 || len(installed) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] || !installed[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func enabledIDsContain(ids []string, id string) bool {
	for _, current := range ids {
		if current == id {
			return true
		}
	}
	return false
}
