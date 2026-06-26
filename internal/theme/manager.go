package theme

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/render"
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

	// 运行时状态：模板渲染器、functions 脚本、恢复信息。
	runtimeMu     sync.Mutex
	themeOpMu     sync.Mutex
	mu            sync.RWMutex
	renderer      *render.Renderer
	log           *slog.Logger
	currentScript *FunctionsScript
	currentAPI    *API
	recoveryInfo  *RecoveryInfo
}

// NewManager 创建主题管理器。themesDir 是主题存放目录（如 "themes"）。
func NewManager(themesDir string, store settingStore, renderer *render.Renderer) (*Manager, error) {
	m := &Manager{
		themesDir: themesDir,
		store:     store,
		themes:    make(map[string]*Theme),
		renderer:  renderer,
		log:       slog.Default().With("component", "theme-manager"),
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
		return errors.Wrap(err, "读取主题目录失败")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(m.themesDir, entry.Name())
		t, err := LoadTheme(dir)
		if err != nil {
			m.log.Warn("无效主题",
				slog.String("dir", dir),
				slog.Any("err", err),
			)
			continue // 跳过无效主题
		}
		m.themes[t.Name] = t
		_ = t.LoadTranslations()
	}
	return nil
}

// LoadTranslations 加载所有已安装主题自带的翻译文件。
func (m *Manager) LoadTranslations() error {
	if m == nil {
		return nil
	}
	for _, t := range m.List() {
		if err := t.LoadTranslations(); err != nil {
			return err
		}
	}
	return nil
}

// RebuildTranslations 重新构建应用域和所有主题域的翻译映射。
// 适用于主题安装/删除后清理失效 domain，同时保留当前 locale 配置。
func (m *Manager) RebuildTranslations() error {
	if m == nil {
		return nil
	}
	return i18n.RebuildDomains(m.LoadTranslations)
}

// List 返回所有已安装的主题列表，按名称排序。
func (m *Manager) List() []*Theme {
	m.mu.RLock()
	defer m.mu.RUnlock()

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
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.themes[name]
}

// Current 返回当前激活的主题名称。
func (m *Manager) Current(ctx context.Context) *Theme {
	name, err := m.store.GetSetting(ctx, settingCurrentTheme)
	if err != nil || name == "" {
		name = defaultThemeName
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	t := m.themes[name]
	if t == nil {
		t = m.themes[defaultThemeName]
	}
	return t
}

// CurrentWithPreview 返回预览主题（如果 previewName 有效），否则返回当前激活主题。
// previewName 为空时等同于 Current()。
func (m *Manager) CurrentWithPreview(ctx context.Context, previewName string) *Theme {
	if previewName != "" {
		if t := m.Get(previewName); t != nil {
			return t
		}
	}
	return m.Current(ctx)
}

// Activate 激活指定主题并持久化。
func (m *Manager) Activate(ctx context.Context, name string) error {
	if m.Get(name) == nil {
		return errors.Errorf("theme %q not found", name)
	}
	return m.store.SetSetting(ctx, settingCurrentTheme, name)
}

// Install 从已解压的目录安装主题。dir 是主题根目录（含 theme.yaml）。
// 安装过程：校验 → 暂存复制 → 备份旧目录 → 原子替换。
func (m *Manager) Install(dir string) (*Theme, error) {
	m.themeOpMu.Lock()
	defer m.themeOpMu.Unlock()

	t, err := LoadTheme(dir)
	if err != nil {
		return nil, err
	}
	// 校验模板目录存在
	if !t.HasTemplates() {
		return nil, errors.New("theme must contain a templates/ directory")
	}
	if err := os.MkdirAll(m.themesDir, 0o755); err != nil {
		return nil, errors.Wrap(err, "create themes dir")
	}

	stagingDir, err := os.MkdirTemp(m.themesDir, ".install-"+t.Name+"-*")
	if err != nil {
		return nil, errors.Wrap(err, "create theme staging dir")
	}
	stagingInstalled := false
	defer func() {
		if !stagingInstalled {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	if err := copyDir(dir, stagingDir); err != nil {
		return nil, errors.Wrap(err, "copy theme to staging dir")
	}
	stagedTheme, err := LoadTheme(stagingDir)
	if err != nil {
		return nil, errors.Wrap(err, "load staged theme")
	}
	if stagedTheme.Name != t.Name {
		return nil, errors.Errorf("staged theme name changed from %q to %q", t.Name, stagedTheme.Name)
	}
	if !stagedTheme.HasTemplates() {
		return nil, errors.New("theme must contain a templates/ directory")
	}

	themeName := t.Name
	targetDir := filepath.Join(m.themesDir, themeName)
	m.mu.RLock()
	oldTheme := m.themes[themeName]
	m.mu.RUnlock()
	backupDir, err := backupExistingThemeDir(targetDir)
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
		return nil, errors.Wrap(err, "replace theme dir")
	}
	stagingInstalled = true

	// 重新加载（确保 Dir 指向正确位置）
	t, err = LoadTheme(targetDir)
	if err != nil {
		if rbErr := rollbackThemeInstall(targetDir, backupDir); rbErr != nil {
			m.mu.Lock()
			delete(m.themes, themeName)
			m.mu.Unlock()
			return nil, errors.Wrapf(err, "load installed theme; rollback failed: %v", rbErr)
		}
		return nil, errors.Wrap(err, "load installed theme")
	}
	if err := t.LoadTranslations(); err != nil {
		if rbErr := rollbackThemeInstall(targetDir, backupDir); rbErr != nil {
			m.mu.Lock()
			delete(m.themes, themeName)
			m.mu.Unlock()
			return nil, errors.Wrapf(err, "load theme translations; rollback failed: %v", rbErr)
		}
		m.mu.Lock()
		if oldTheme != nil {
			m.themes[themeName] = oldTheme
		} else {
			delete(m.themes, themeName)
		}
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Lock()
	m.themes[themeName] = t
	m.mu.Unlock()
	if err := m.RebuildTranslations(); err != nil {
		m.mu.Lock()
		if oldTheme != nil {
			m.themes[themeName] = oldTheme
		} else {
			delete(m.themes, themeName)
		}
		m.mu.Unlock()
		rbErr := rollbackThemeInstall(targetDir, backupDir)
		restoreErr := m.RebuildTranslations()
		if rbErr != nil || restoreErr != nil {
			return nil, errors.Wrapf(err, "rebuild theme translations; rollback failed: %v; restore translations failed: %v", rbErr, restoreErr)
		}
		return nil, err
	}
	return t, nil
}

// Delete 删除指定主题。如果删除的是当前激活主题，不会自动切换。
func (m *Manager) Delete(name string) error {
	m.themeOpMu.Lock()
	defer m.themeOpMu.Unlock()

	m.mu.RLock()
	t, ok := m.themes[name]
	m.mu.RUnlock()
	if !ok {
		return errors.Errorf("theme %q not found", name)
	}
	if name == defaultThemeName {
		return errors.New("cannot delete the default theme")
	}
	backupDir, err := backupExistingThemeDir(t.Dir)
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.themes, name)
	m.mu.Unlock()
	if err := m.RebuildTranslations(); err != nil {
		m.mu.Lock()
		m.themes[name] = t
		m.mu.Unlock()
		if backupDir != "" {
			_ = os.Rename(backupDir, t.Dir)
		}
		restoreErr := m.RebuildTranslations()
		if restoreErr != nil {
			return errors.Wrapf(err, "rebuild theme translations after delete; restore translations failed: %v", restoreErr)
		}
		return err
	}
	if backupDir != "" {
		if err := os.RemoveAll(backupDir); err != nil {
			m.mu.Lock()
			m.themes[name] = t
			m.mu.Unlock()
			if _, statErr := os.Stat(t.Dir); os.IsNotExist(statErr) {
				_ = os.Rename(backupDir, t.Dir)
			}
			_ = m.RebuildTranslations()
			return errors.Wrap(err, "remove theme backup dir")
		}
	}
	return nil
}

// ThemesDir 返回主题存放目录路径。
func (m *Manager) ThemesDir() string {
	return m.themesDir
}

func backupExistingThemeDir(targetDir string) (string, error) {
	if _, err := os.Stat(targetDir); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", errors.Wrap(err, "stat existing theme dir")
	}
	backupDir, err := os.MkdirTemp(filepath.Dir(targetDir), ".backup-"+filepath.Base(targetDir)+"-*")
	if err != nil {
		return "", errors.Wrap(err, "create theme backup dir")
	}
	if err := os.Remove(backupDir); err != nil {
		return "", errors.Wrap(err, "prepare theme backup dir")
	}
	if err := os.Rename(targetDir, backupDir); err != nil {
		return "", errors.Wrap(err, "backup existing theme dir")
	}
	return backupDir, nil
}

func rollbackThemeInstall(targetDir, backupDir string) error {
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
				return errors.Errorf("unsupported theme file type: %s", srcPath)
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
