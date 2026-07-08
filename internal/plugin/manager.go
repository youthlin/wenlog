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
	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/i18n"
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
	hooks      *hook.Hooks
	scripts    map[string]*FunctionsScript
	loadErrors map[string]string
	enabledIDs []string
	enabledSet bool
	log        *slog.Logger
	mu         sync.RWMutex
	pluginOpMu sync.Mutex // 串行化 Install/Delete 操作
}

// NewManager 创建插件管理器。pluginsDir 是插件存放目录（如 "plugins"）。
func NewManager(pluginsDir string, store hook.SettingStore) (*Manager, error) {
	m := &Manager{
		pluginsDir: pluginsDir,
		store:      store,
		plugins:    make(map[string]*Plugin),
		hooks:      hook.NewRegistry(),
		scripts:    make(map[string]*FunctionsScript),
		loadErrors: make(map[string]string),
		log:        slog.Default().With("component", "plugin-manager"),
	}
	if err := m.scan(context.Background()); err != nil {
		return nil, err
	}
	return m, nil
}

// scan 扫描 pluginsDir 下所有子目录，加载 plugin.yaml。
func (m *Manager) scan(ctx context.Context) error {
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
			m.log.WarnContext(ctx, "无效插件",
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

// GetRegistry 返回插件系统共享的 Hook Registry（实例稳定，不会因重载而替换）。
func (m *Manager) GetRegistry() hook.Registry {
	if m == nil {
		return nil
	}
	return m.hooks
}

// LoadEnabledFunctions 按启用顺序编译插件 functions，成功后原子替换到共享 Registry。
// 编译到临时 Registry 中，失败时不影响当前已加载的处理器。
func (m *Manager) LoadEnabledFunctions(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	tmp := hook.NewRegistry()
	scripts := make(map[string]*FunctionsScript)
	loadErrors := make(map[string]string)
	ids := m.enabledIDsLocked(ctx)
	for _, id := range ids {
		p := m.plugins[id]
		if p == nil {
			continue
		}
		script, err := CompileFunctions(ctx, p, tmp, m.log)
		if err != nil {
			loadErrors[id] = err.Error()
			m.loadErrors = loadErrors
			return errors.Wrapf(err, "加载插件[%s]运行时失败", id)
		}
		if script != nil {
			scripts[id] = script
		}
	}
	m.hooks.ReplaceAllFrom(tmp)
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

	hooks := hook.NewRegistry()
	tmp, err := CompileFunctions(ctx, p, hooks, m.log)
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

// Install 从已解压的目录安装插件。dir 是插件根目录（含 plugin.yaml）。
func (m *Manager) Install(dir string) (*Plugin, error) {
	m.pluginOpMu.Lock()
	defer m.pluginOpMu.Unlock()

	p, err := LoadPlugin(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.pluginsDir, 0o755); err != nil {
		return nil, errors.Wrap(err, "create plugins dir")
	}

	stagingDir, err := os.MkdirTemp(m.pluginsDir, ".install-"+p.ID+"-*")
	if err != nil {
		return nil, errors.Wrap(err, "create plugin staging dir")
	}
	stagingInstalled := false
	defer func() {
		if !stagingInstalled {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	if err := copyDir(dir, stagingDir); err != nil {
		return nil, errors.Wrap(err, "copy plugin to staging dir")
	}
	stagedPlugin, err := LoadPlugin(stagingDir)
	if err != nil {
		return nil, errors.Wrap(err, "load staged plugin")
	}
	if stagedPlugin.ID != p.ID {
		return nil, errors.Errorf("staged plugin id changed from %q to %q", p.ID, stagedPlugin.ID)
	}

	pluginID := p.ID
	targetDir := filepath.Join(m.pluginsDir, pluginID)
	m.mu.RLock()
	oldPlugin := m.plugins[pluginID]
	m.mu.RUnlock()
	backupDir, err := backupExistingPluginDir(targetDir)
	if err != nil {
		return nil, err
	}
	if backupDir != "" {
		defer os.RemoveAll(backupDir)
	}
	if err := os.Rename(stagingDir, targetDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, targetDir)
		}
		return nil, errors.Wrap(err, "replace plugin dir")
	}
	stagingInstalled = true

	// 重新加载（确保 Dir 指向正确位置）
	p, err = LoadPlugin(targetDir)
	if err != nil {
		if rbErr := rollbackPluginInstall(targetDir, backupDir); rbErr != nil {
			m.mu.Lock()
			delete(m.plugins, pluginID)
			m.mu.Unlock()
			return nil, errors.Wrapf(err, "load installed plugin; rollback failed: %v", rbErr)
		}
		return nil, errors.Wrap(err, "load installed plugin")
	}
	if err := p.LoadTranslations(); err != nil {
		if rbErr := rollbackPluginInstall(targetDir, backupDir); rbErr != nil {
			m.mu.Lock()
			delete(m.plugins, pluginID)
			m.mu.Unlock()
			return nil, errors.Wrapf(err, "load plugin translations; rollback failed: %v", rbErr)
		}
		m.mu.Lock()
		if oldPlugin != nil {
			m.plugins[pluginID] = oldPlugin
		} else {
			delete(m.plugins, pluginID)
		}
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Lock()
	m.plugins[pluginID] = p
	m.invalidateEnabledIDsCacheLocked()
	m.mu.Unlock()
	if err := m.RebuildTranslations(); err != nil {
		m.mu.Lock()
		if oldPlugin != nil {
			m.plugins[pluginID] = oldPlugin
		} else {
			delete(m.plugins, pluginID)
		}
		m.invalidateEnabledIDsCacheLocked()
		m.mu.Unlock()
		rbErr := rollbackPluginInstall(targetDir, backupDir)
		restoreErr := m.RebuildTranslations()
		if rbErr != nil || restoreErr != nil {
			return nil, errors.Wrapf(err, "rebuild plugin translations; rollback failed: %v; restore translations failed: %v", rbErr, restoreErr)
		}
		return nil, err
	}
	return p, nil
}

// Delete 删除指定插件目录并从内存中移除。
func (m *Manager) Delete(id string) error {
	m.pluginOpMu.Lock()
	defer m.pluginOpMu.Unlock()

	m.mu.RLock()
	p, ok := m.plugins[id]
	m.mu.RUnlock()
	if !ok {
		return errors.Errorf("plugin %q not found", id)
	}
	backupDir, err := backupExistingPluginDir(p.Dir)
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.plugins, id)
	m.invalidateEnabledIDsCacheLocked()
	m.mu.Unlock()
	if err := m.RebuildTranslations(); err != nil {
		m.mu.Lock()
		m.plugins[id] = p
		m.invalidateEnabledIDsCacheLocked()
		m.mu.Unlock()
		if backupDir != "" {
			_ = os.Rename(backupDir, p.Dir)
		}
		restoreErr := m.RebuildTranslations()
		if restoreErr != nil {
			return errors.Wrapf(err, "rebuild plugin translations after delete; restore translations failed: %v", restoreErr)
		}
		return err
	}
	if backupDir != "" {
		if err := os.RemoveAll(backupDir); err != nil {
			m.mu.Lock()
			m.plugins[id] = p
			m.invalidateEnabledIDsCacheLocked()
			m.mu.Unlock()
			if _, statErr := os.Stat(p.Dir); os.IsNotExist(statErr) {
				_ = os.Rename(backupDir, p.Dir)
			}
			_ = m.RebuildTranslations()
			return errors.Wrap(err, "remove plugin backup dir")
		}
	}
	return nil
}

// PluginsDir 返回插件存放目录路径。
func (m *Manager) PluginsDir() string {
	return m.pluginsDir
}

func backupExistingPluginDir(targetDir string) (string, error) {
	if _, err := os.Stat(targetDir); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", errors.Wrap(err, "stat existing plugin dir")
	}
	backupDir, err := os.MkdirTemp(filepath.Dir(targetDir), ".backup-"+filepath.Base(targetDir)+"-*")
	if err != nil {
		return "", errors.Wrap(err, "create plugin backup dir")
	}
	if err := os.Remove(backupDir); err != nil {
		return "", errors.Wrap(err, "prepare plugin backup dir")
	}
	if err := os.Rename(targetDir, backupDir); err != nil {
		return "", errors.Wrap(err, "backup existing plugin dir")
	}
	return backupDir, nil
}

func rollbackPluginInstall(targetDir, backupDir string) error {
	_ = os.RemoveAll(targetDir)
	if backupDir == "" {
		return nil
	}
	return os.Rename(backupDir, targetDir)
}

// copyDir 递归复制目录到一个空目标目录。
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.Errorf("unsupported plugin file type: %s", srcPath)
			}
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
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
	if m.enabledSet {
		ids := cloneIDs(m.enabledIDs)
		m.mu.RUnlock()
		return ids
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabledIDsLocked(ctx)
}

func (m *Manager) enabledIDsLocked(ctx context.Context) []string {
	if m.enabledSet {
		return cloneIDs(m.enabledIDs)
	}
	raw, err := m.store.GetSetting(ctx, settingEnabledPlugins)
	var ids []string
	if err != nil || raw == "" {
		ids = m.defaultEnabledIDsLocked()
	} else if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		ids = nil
	} else {
		installed := make(map[string]bool, len(m.plugins))
		for id := range m.plugins {
			installed[id] = true
		}
		ids = filterInstalledIDs(ids, installed)
	}
	m.enabledIDs = cloneIDs(ids)
	m.enabledSet = true
	return cloneIDs(ids)
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
	if err := m.store.SetSetting(ctx, settingEnabledPlugins, string(data)); err != nil {
		return err
	}
	m.mu.Lock()
	m.enabledIDs = cloneIDs(ids)
	m.enabledSet = true
	m.mu.Unlock()
	return nil
}

func (m *Manager) invalidateEnabledIDsCacheLocked() {
	m.enabledIDs = nil
	m.enabledSet = false
}

func cloneIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	copy(out, ids)
	return out
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
