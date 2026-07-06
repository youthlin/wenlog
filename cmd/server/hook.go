package main

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/middleware"
	"github.com/youthlin/wenlog/internal/plugin"
	"github.com/youthlin/wenlog/internal/render"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/theme"
	"github.com/youthlin/wenlog/web"
	"github.com/youthlin/wenlog/web/plugins"
	"gopkg.in/yaml.v3"
)

// initHook 初始化主题与插件两大扩展体系，并串联它们的依赖关系。
//
// 初始化顺序有严格依赖：
//
//  1. 创建主题管理器、插件管理器（各自扫描磁盘目录，不配置 renderer）
//  2. 将插件 Hook Registry 注入 theme.Manager
//     —— 此后主题 functions.goyaegi 注册的 action/filter 会写入插件 Registry
//  3. 集中配置 renderer 模板运行时（BindTemplateFunctions → ThemeWidgets / ThemeOption / Hooks）
//  4. 加载当前主题（模板 + functions.goyaegi）
//     —— 此时 Hook Registry 已就绪，主题脚本可正常注册 hook
//  5. 构建统一组件注册表（内置 → 主题 → 插件），注入 renderer
//  6. 注册主题/插件静态资源路由
func initHook(r *gin.Engine, st *store.Store, renderer *render.Renderer) (
	tm *theme.Manager,
	pm *plugin.Manager,
) {
	var err error
	var ctx = context.Background()

	// 1. 初始化主题和插件管理器（仅创建实例，不配置 renderer）
	tm, err = initThemeManager(st, renderer)
	if err != nil {
		slog.ErrorContext(bg, "初始化主题管理器失败", slog.Any("error", err))
		os.Exit(1)
	}
	pm, err = initPluginManager(st)
	if err != nil {
		slog.ErrorContext(bg, "初始化插件管理器失败", slog.Any("error", err))
		os.Exit(1)
	}

	// 2. 将插件 Hook Registry 注入 theme.Manager（Registry 实例稳定，直接持有）
	tm.SetHookRegistry(pm.GetRegistry())
	// 插件组件声明供主题管理器合并到可用组件列表
	tm.SetPluginWidgetsProvider(pm.EnabledWidgetDecls)

	// 3. 集中配置 renderer 模板运行时（ThemeWidgets / ThemeOption / Hooks）
	tm.BindTemplateFunctions()

	// 4. 加载当前主题的模板和 functions.goyaegi
	//    必须在 Hook Registry 注入之后执行，否则主题脚本注册 hook 会失败
	if err := tm.LoadTheme(ctx, ""); err != nil {
		slog.ErrorContext(bg, "加载主题 hook 失败", slog.Any("error", err))
	}

	// 5. 构建统一组件注册表：内置 → 主题 → 插件，注入 renderer
	populateWidgetRegistry(ctx, tm, pm, renderer)

	// 6. 注册主题和插件静态资源路由
	registerThemeAssetsRoute(r, tm)
	registerPluginAssetsRoute(r, pm)
	return
}

// populateWidgetRegistry 构建统一组件注册表并注入 renderer。
// 注册顺序：内置组件 → 主题组件 → 插件组件（后注册的覆盖先注册的同名组件）。
func populateWidgetRegistry(ctx context.Context, tm *theme.Manager, pm *plugin.Manager, renderer *render.Renderer) {
	widgetRegistry := hook.NewWidgetRegistry()
	theme.RegisterBuiltins(widgetRegistry)

	if t := tm.Current(ctx); t != nil {
		theme.RegisterThemeWidgets(widgetRegistry, t)
	}
	pm.RegisterPluginWidgets(ctx, widgetRegistry)

	renderer.SetWidgetResolver(widgetRegistry.Get)
}

func initThemeManager(st *store.Store, tplRenderer *render.Renderer) (*theme.Manager, error) {
	ensureThemesOnDisk()
	tm, err := theme.NewManager("themes", st, tplRenderer)
	if err != nil {
		return nil, err
	}

	// 设置默认主题 embed FS，供 ResetToDefault 时从 embed 加载。
	if defaultThemeFS, err := fs.Sub(web.Themes, "themes/default/templates"); err == nil {
		tplRenderer.SetDefaultThemeFS(defaultThemeFS)
	}
	return tm, nil
}

func initPluginManager(st *store.Store) (*plugin.Manager, error) {
	ensurePluginsOnDisk()
	pm, err := plugin.NewManager("plugins", st)
	if err != nil {
		return nil, err
	}
	if err := pm.LoadEnabledFunctions(context.Background()); err != nil {
		return nil, err
	}

	return pm, nil
}

// ensureThemesOnDisk 确保所有内嵌主题在磁盘上存在。
// 首次启动会释放内嵌主题；如果内嵌主题版本更新，则覆盖运行时目录，避免旧模板缺少新增 hook 点。
func ensureThemesOnDisk() {
	entries, err := fs.ReadDir(web.Themes, "themes")
	if err != nil {
		slog.WarnContext(bg, "读取内嵌主题失败", "error", err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		themeName := entry.Name()
		themeYAML := filepath.Join("themes", themeName, "theme.yaml")
		if _, err := os.Stat(themeYAML); err == nil {
			if !bundledThemeNewer(themeYAML, filepath.Join("themes", themeName, "theme.yaml")) {
				continue // 已存在且版本不低于内嵌版本，保留用户可能做过的本地调整
			}
			if err := os.RemoveAll(filepath.Join("themes", themeName)); err != nil {
				slog.WarnContext(bg, "清理旧运行时主题失败", "theme", themeName, "error", err)
				continue
			}
		}
		if err := releaseBundledTheme(themeName); err != nil {
			slog.WarnContext(bg, "释放内嵌主题失败", "theme", themeName, "error", err)
		}
	}
}

func bundledThemeNewer(diskYAML, embedPath string) bool {
	disk, err := readThemeMetaFromDisk(diskYAML)
	if err != nil {
		return false
	}
	bundled, err := readThemeMetaFromEmbed(embedPath)
	if err != nil {
		return false
	}
	return disk.Name == bundled.Name && compareVersion(disk.Version, bundled.Version) < 0
}

type themeMeta struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

func readThemeMetaFromDisk(path string) (themeMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return themeMeta{}, err
	}
	var meta themeMeta
	return meta, yaml.Unmarshal(data, &meta)
}

func readThemeMetaFromEmbed(path string) (themeMeta, error) {
	data, err := fs.ReadFile(web.Themes, path)
	if err != nil {
		return themeMeta{}, err
	}
	var meta themeMeta
	return meta, yaml.Unmarshal(data, &meta)
}

func compareVersion(a, b string) int {
	as := strings.Split(strings.TrimSpace(a), ".")
	bs := strings.Split(strings.TrimSpace(b), ".")
	n := max(len(bs), len(as))
	for i := range n {
		av := versionPart(as, i)
		bv := versionPart(bs, i)
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}
	return 0
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	v, _ := strconv.Atoi(strings.TrimSpace(parts[i]))
	return v
}

func releaseBundledTheme(themeName string) error {
	themeDir := filepath.Join("themes", themeName)
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		return err
	}
	prefix := filepath.Join("themes", themeName)
	return fs.WalkDir(web.Themes, prefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(web.Themes, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// ensurePluginsOnDisk 确保所有内嵌插件在磁盘上存在。
// 插件与主题一样以磁盘目录作为运行时来源：首次启动释放内置插件；内置插件版本更新时覆盖运行时目录。
func ensurePluginsOnDisk() {
	entries, err := fs.ReadDir(plugins.Plugins, ".")
	if err != nil {
		slog.WarnContext(bg, "读取内嵌插件失败", "error", err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginID := entry.Name()
		pluginYAML := filepath.Join("plugins", pluginID, "plugin.yaml")
		if _, err := os.Stat(pluginYAML); err == nil {
			if !bundledPluginNewer(pluginYAML, pluginID+"/plugin.yaml") {
				continue // 已存在且版本不低于内嵌版本，保留用户可能做过的本地调整
			}
			if err := os.RemoveAll(filepath.Join("plugins", pluginID)); err != nil {
				slog.WarnContext(bg, "清理旧运行时插件失败", "plugin", pluginID, "error", err)
				continue
			}
		}
		if err := releaseBundledPlugin(pluginID); err != nil {
			slog.WarnContext(bg, "释放内嵌插件失败", "plugin", pluginID, "error", err)
		}
	}
}

func bundledPluginNewer(diskYAML, embedPath string) bool {
	disk, err := readPluginMetaFromDisk(diskYAML)
	if err != nil {
		return false
	}
	bundled, err := readPluginMetaFromEmbed(embedPath)
	if err != nil {
		return false
	}
	return disk.ID == bundled.ID && compareVersion(disk.Version, bundled.Version) < 0
}

type pluginMeta struct {
	ID      string `yaml:"id"`
	Version string `yaml:"version"`
}

func readPluginMetaFromDisk(path string) (pluginMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pluginMeta{}, err
	}
	var meta pluginMeta
	return meta, yaml.Unmarshal(data, &meta)
}

func readPluginMetaFromEmbed(path string) (pluginMeta, error) {
	data, err := fs.ReadFile(plugins.Plugins, path)
	if err != nil {
		return pluginMeta{}, err
	}
	var meta pluginMeta
	return meta, yaml.Unmarshal(data, &meta)
}

func releaseBundledPlugin(pluginID string) error {
	pluginDir := filepath.Join("plugins", pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(plugins.Plugins, pluginID, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join("plugins", path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(plugins.Plugins, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func registerThemeAssetsRoute(r *gin.Engine, tm *theme.Manager) {
	// 当前主题静态资源：/theme-assets/... → themes/{current}/assets/...
	// 管理员预览主题时优先使用预览主题的资源。
	// 主题资源带版本号（?v=1.0.0），可长期缓存 1 年。
	r.GET("/theme-assets/*filepath", func(c *gin.Context) {
		current := tm.Current(c)
		if previewName := middleware.GetPreviewTheme(c); previewName != "" {
			if pt := tm.Get(previewName); pt != nil && pt.HasAssets() {
				current = pt
			}
		}
		if current == nil || !current.HasAssets() {
			c.Status(http.StatusNotFound)
			return
		}
		name := strings.TrimPrefix(filepath.Clean(c.Param("filepath")), string(filepath.Separator))
		if name == "." || strings.HasPrefix(name, "..") {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "public, max-age=31536000")
		c.File(filepath.Join(current.AssetsDir(), name))
	})
}

func registerPluginAssetsRoute(r *gin.Engine, pm *plugin.Manager) {
	// 插件静态资源：/plugin-assets/{plugin_id}/{path} → plugins/{plugin_id}/assets/{path}
	// 只有已启用插件的资源会对外暴露。
	r.GET("/plugin-assets/:plugin/*filepath", func(c *gin.Context) {
		id := c.Param("plugin")
		p := pm.Get(id)
		if p == nil || !p.HasAssets() || !pluginEnabled(c, pm, id) {
			c.Status(http.StatusNotFound)
			return
		}
		name := strings.TrimPrefix(filepath.Clean(c.Param("filepath")), string(filepath.Separator))
		if name == "." || strings.HasPrefix(name, "..") {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "public, max-age=31536000")
		c.File(filepath.Join(p.AssetsDir(), name))
	})
}

func pluginEnabled(ctx context.Context, pm *plugin.Manager, id string) bool {
	return slices.Contains(pm.EnabledIDs(ctx), id)
}
