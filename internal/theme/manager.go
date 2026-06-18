package theme

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/cockroachdb/errors"
)

// settingStore 是 Manager 需要的设置存储接口。
type settingStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

const (
	settingCurrentTheme = "current_theme"
	defaultThemeName    = "default"
)

// Manager 管理主题的安装、激活、删除。
type Manager struct {
	themesDir string
	store     settingStore
	themes    map[string]*Theme // name → Theme
}

// NewManager 创建主题管理器。themesDir 是主题存放目录（如 "themes"）。
func NewManager(themesDir string, store settingStore) (*Manager, error) {
	m := &Manager{
		themesDir: themesDir,
		store:     store,
		themes:    make(map[string]*Theme),
	}
	if err := m.scan(); err != nil {
		return nil, err
	}
	return m, nil
}

// scan 扫描 themesDir 下所有子目录，加载 theme.yaml。
func (m *Manager) scan() error {
	entries, err := os.ReadDir(m.themesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrap(err, "scan themes dir")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(m.themesDir, entry.Name())
		t, err := LoadTheme(dir)
		if err != nil {
			continue // 跳过无效主题
		}
		m.themes[t.Name] = t
	}
	return nil
}

// List 返回所有已安装的主题列表，按名称排序。
func (m *Manager) List() []*Theme {
	names := make([]string, 0, len(m.themes))
	for name := range m.themes {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]*Theme, 0, len(names))
	for _, name := range names {
		result = append(result, m.themes[name])
	}
	return result
}

// Get 按名称获取主题，不存在返回 nil。
func (m *Manager) Get(name string) *Theme {
	return m.themes[name]
}

// Current 返回当前激活的主题名称。
func (m *Manager) Current(ctx context.Context) *Theme {
	name, err := m.store.GetSetting(ctx, settingCurrentTheme)
	if err != nil || name == "" {
		name = defaultThemeName
	}
	t := m.themes[name]
	if t == nil {
		t = m.themes[defaultThemeName]
	}
	return t
}

// Activate 激活指定主题并持久化。
func (m *Manager) Activate(ctx context.Context, name string) error {
	if _, ok := m.themes[name]; !ok {
		return errors.Errorf("theme %q not found", name)
	}
	return m.store.SetSetting(ctx, settingCurrentTheme, name)
}

// Install 从已解压的目录安装主题。dir 是主题根目录（含 theme.yaml）。
// 安装过程：校验 → 复制到 themesDir/{name}/。
func (m *Manager) Install(dir string) (*Theme, error) {
	t, err := LoadTheme(dir)
	if err != nil {
		return nil, err
	}
	// 校验模板目录存在
	if !t.HasTemplates() {
		return nil, errors.New("theme must contain a templates/ directory")
	}
	// 复制到 themesDir
	targetDir := filepath.Join(m.themesDir, t.Name)
	if err := copyDir(dir, targetDir); err != nil {
		return nil, errors.Wrap(err, "copy theme to themes dir")
	}
	// 重新加载（确保 Dir 指向正确位置）
	t, err = LoadTheme(targetDir)
	if err != nil {
		return nil, err
	}
	m.themes[t.Name] = t
	return t, nil
}

// Delete 删除指定主题。如果删除的是当前激活主题，不会自动切换。
func (m *Manager) Delete(name string) error {
	t, ok := m.themes[name]
	if !ok {
		return errors.Errorf("theme %q not found", name)
	}
	if name == defaultThemeName {
		return errors.New("cannot delete the default theme")
	}
	if err := os.RemoveAll(t.Dir); err != nil {
		return errors.Wrap(err, "remove theme dir")
	}
	delete(m.themes, name)
	return nil
}

// ThemesDir 返回主题存放目录路径。
func (m *Manager) ThemesDir() string {
	return m.themesDir
}

// copyDir 递归复制目录。
func copyDir(src, dst string) error {
	// 先删除目标目录（如果存在）
	_ = os.RemoveAll(dst)
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
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}
