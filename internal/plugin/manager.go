package plugin

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/i18n"
)

const settingEnabledPlugins = "enabled_plugins"

// settingStore 是 Manager 需要的设置存储接口。
type settingStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

// Manager 管理插件扫描、启用列表和共享 Hook Registry。
type Manager struct {
	pluginsDir string
	store      settingStore
	plugins    map[string]*Plugin
	hooks      *Registry
	scripts    map[string]*FunctionsScript
	loadErrors map[string]string
	log        *slog.Logger
	mu         sync.RWMutex
}

// NewManager 创建插件管理器。pluginsDir 是插件存放目录（如 "plugins"）。
func NewManager(pluginsDir string, store settingStore) (*Manager, error) {
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
		registerPluginWidgets(hooks, p)
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
	for _, p := range m.List() {
		if err := p.LoadTranslations(); err != nil {
			return err
		}
	}
	return nil
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

func registerPluginWidgets(hooks *Registry, p *Plugin) {
	if hooks == nil || p == nil || len(p.Widgets) == 0 {
		return
	}
	widgets := append([]WidgetDecl(nil), p.Widgets...)
	source := Source{Type: SourcePlugin, ID: p.ID}
	hooks.AddFilter("widgets.available", func(value any, args ...any) any {
		return appendWidgetDecls(value, widgets, p.ID)
	}, source)
}

func appendWidgetDecls(value any, widgets []WidgetDecl, pluginID string) any {
	if len(widgets) == 0 {
		return value
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return value
	}
	out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len()+len(widgets))
	reflect.Copy(out, rv)
	for _, w := range widgets {
		item, ok := makeWidgetDeclValue(rv.Type().Elem(), w, pluginID)
		if !ok {
			continue
		}
		out = reflect.Append(out, item)
	}
	return out.Interface()
}

func makeWidgetDeclValue(t reflect.Type, w WidgetDecl, pluginID string) (reflect.Value, bool) {
	ptr := false
	if t.Kind() == reflect.Pointer {
		ptr = true
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	item := reflect.New(t).Elem()
	setStringField(item, "ID", w.ID)
	setStringField(item, "Label", w.Label)
	setStringField(item, "Source", "plugin:"+pluginID)
	setStringField(item, "PluginID", pluginID)
	setOptionDecls(item.FieldByName("Options"), w.Options)
	if ptr {
		p := reflect.New(t)
		p.Elem().Set(item)
		return p, true
	}
	return item, true
}

func setOptionDecls(field reflect.Value, options []OptionDecl) {
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.Slice || len(options) == 0 {
		return
	}
	out := reflect.MakeSlice(field.Type(), 0, len(options))
	for _, opt := range options {
		item, ok := makeOptionDeclValue(field.Type().Elem(), opt)
		if ok {
			out = reflect.Append(out, item)
		}
	}
	field.Set(out)
}

func makeOptionDeclValue(t reflect.Type, opt OptionDecl) (reflect.Value, bool) {
	ptr := false
	if t.Kind() == reflect.Pointer {
		ptr = true
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	item := reflect.New(t).Elem()
	setStringField(item, "ID", opt.ID)
	setStringField(item, "Type", opt.Type)
	setStringField(item, "Label", opt.Label)
	setStringField(item, "Description", opt.Description)
	setStringField(item, "Default", opt.Default)
	setFloatPtrField(item, "Min", opt.Min)
	setFloatPtrField(item, "Max", opt.Max)
	if ptr {
		p := reflect.New(t)
		p.Elem().Set(item)
		return p, true
	}
	return item, true
}

func setStringField(item reflect.Value, name, value string) {
	field := item.FieldByName(name)
	if field.IsValid() && field.CanSet() && field.Kind() == reflect.String {
		field.SetString(value)
	}
}

func setFloatPtrField(item reflect.Value, name string, value *float64) {
	field := item.FieldByName(name)
	if value == nil || !field.IsValid() || !field.CanSet() || field.Kind() != reflect.Pointer || field.Type().Elem().Kind() != reflect.Float64 {
		return
	}
	v := reflect.New(field.Type().Elem())
	v.Elem().SetFloat(*value)
	field.Set(v)
}
