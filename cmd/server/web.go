package main

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"

	"github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/config"
	"github.com/youthlin/blog/internal/handler"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/plugin"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/theme"
	"github.com/youthlin/blog/internal/util"
	"github.com/youthlin/blog/web"
	bundledplugins "github.com/youthlin/blog/web/plugins"
)

// serve 启动 web 服务器
func serve(cfg *config.Config, st *store.Store) error {
	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: createWebHandler(cfg, st),
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	util.Go(func() {
		<-quit
		slog.Info("系统正在退出...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("系统退出异常", slog.Any("error", err))
		}
	})

	slog.Info("服务监听中...", slog.String("addr", cfg.Addr))
	err := srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// createWebHandler 创建并注册路由。
func createWebHandler(cfg *config.Config, st *store.Store) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.ContextWithFallback = true

	// 中间件
	r.Use(
		middleware.Recover(),
		middleware.TraceID(),
		middleware.Logger(),
		middleware.Metrics(),
		middleware.SQLTracer(),
		middleware.Session(st)(),
		i18n.Middleware(),
	)

	// 路由注册
	register(r, cfg, st)

	return r
}

func register(r *gin.Engine, cfg *config.Config, st *store.Store) {
	// 监控与健康检查。/metrics 使用后台可配置密码保护,避免公网暴露指标。
	r.GET("/metrics", middleware.MetricsBasicAuth(st), gin.WrapH(promhttp.Handler()))
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// 静态资源
	// 历史图片(原路径)+ css/js。开发环境优先直接读磁盘,避免每次改样式/JS 都重启。
	// 添加 Cache-Control 头：历史图片/上传文件 1 天，全局资源 1 天。
	pubContentFs := http.Dir(filepath.Join(cfg.PublicDir, "wp-content"))
	r.GET("/wp-content/*filepath", cacheStatic(pubContentFs, 86400))
	r.GET("/uploads/*filepath", cacheStatic(http.Dir(filepath.Join(cfg.PublicDir, "uploads")), 86400))
	assetLocalFS := assetFS()
	r.GET("/assets/*filepath", cacheStatic(assetLocalFS, 86400))

	// 模板与前端资源
	// 开发环境优先直接读磁盘。若存在本地模板目录,后台会显示 hot 状态并允许手动重新解析;
	// 静态资源仍然直接读磁盘即时生效。缺失时统一回退到 embed。
	tplRenderer, err := templateRenderer()
	if err != nil {
		slog.Error("加载模板失败", slog.Any("error", err))
		os.Exit(1)
	}
	r.HTMLRender = tplRenderer

	tm, err := initThemeManager(st, tplRenderer)
	if err != nil {
		slog.Error("初始化主题管理器失败", slog.Any("error", err))
		os.Exit(1)
	}
	pm, err := initPluginManager(st, tplRenderer)
	if err != nil {
		slog.Error("初始化插件管理器失败", slog.Any("error", err))
		os.Exit(1)
	}
	tm.SetHookRegistry(pm.Hooks())
	tm.SetPluginWidgetsProvider(pm.EnabledWidgetDecls)
	// 插件 Hook Registry 和组件声明提供者就绪后，再加载当前主题的 functions。
	if err := tm.LoadTheme(context.Background(), ""); err != nil {
		slog.Error("加载主题 hook 失败", slog.Any("error", err))
	}

	// 前台
	pub := handler.NewPublic(st, cfg, tm, tplRenderer)
	registerPublicRoutes(r, pub)

	// 认证（独立于前台和后台）
	auth := handler.NewAuth(st)
	rateLimiter := middleware.NewMemoryRateLimiter()
	registerAuthRoutes(r, auth, rateLimiter)

	// 后台
	adm := handler.NewAdmin(st, cfg, tplRenderer, assetLocalFS, tm, pm)
	registerAdminRoutes(r, adm, auth, st)

	registerThemeAssetsRoute(r, tm)
	registerPluginAssetsRoute(r, pm)
}

// cacheStatic 返回一个 gin handler，从 fsys 提供静态文件并设置 Cache-Control 头。
func cacheStatic(fsys http.FileSystem, maxAgeSeconds int) gin.HandlerFunc {
	server := http.FileServer(fsys)
	return func(c *gin.Context) {
		c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", maxAgeSeconds))
		// 去掉 Gin 路由前缀，让 http.FileServer 正确处理路径
		c.Request.URL.Path = c.Param("filepath")
		server.ServeHTTP(c.Writer, c.Request)
	}
}

func assetFS() *handler.LocalFirstFileSystem {
	assetsFS, err := fs.Sub(web.Assets, "assets")
	if err != nil {
		panic(err)
	}
	fsys := handler.NewLocalFirstFileSystem("web/assets", http.FS(assetsFS))
	if _, err := os.Stat(fsys.Dir); err == nil {
		fsys.SetHot(true)
	}
	return fsys
}

func templateRenderer() (*render.Renderer, error) {
	if _, err := os.Stat("web/templates"); err == nil {
		return render.NewHot("web/templates")
	}
	tplFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		return nil, err
	}
	return render.New(tplFS)
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
	tm.BindTemplateFunctions()
	return tm, nil
}

func initPluginManager(st *store.Store, tplRenderer *render.Renderer) (*plugin.Manager, error) {
	ensurePluginsOnDisk()
	pm, err := plugin.NewManager("plugins", st)
	if err != nil {
		return nil, err
	}
	if err := pm.LoadEnabledFunctions(context.Background()); err != nil {
		return nil, err
	}

	tplRenderer.SetHookProvider(pm.Hooks())
	tplRenderer.SetPluginWidgetProvider(func(ctx *render.RequestContext, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool) {
		reqCtx := renderRequestContext(ctx)
		if ctx != nil {
			if loader, ok := ctx.ThemeLoader.(*store.DataLoader); ok {
				reqCtx = hook.WithDataLoader(reqCtx, loader)
			}
		}
		return pm.RenderWidget(reqCtx, pluginID, widgetID, options, data)
	})
	return pm, nil
}

func renderRequestContext(ctx *render.RequestContext) context.Context {
	if ctx != nil && ctx.Context != nil {
		return ctx.Context
	}
	return context.Background()
}

// ensureThemesOnDisk 确保所有内嵌主题在磁盘上存在。
// 首次启动会释放内嵌主题；如果内嵌主题版本更新，则覆盖运行时目录，避免旧模板缺少新增 hook 点。
func ensureThemesOnDisk() {
	entries, err := fs.ReadDir(web.Themes, "themes")
	if err != nil {
		slog.Warn("读取内嵌主题失败", "error", err)
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
				slog.Warn("清理旧运行时主题失败", "theme", themeName, "error", err)
				continue
			}
		}
		if err := releaseBundledTheme(themeName); err != nil {
			slog.Warn("释放内嵌主题失败", "theme", themeName, "error", err)
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

func compareVersion(a, b string) int {
	as := strings.Split(strings.TrimSpace(a), ".")
	bs := strings.Split(strings.TrimSpace(b), ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
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

// ensurePluginsOnDisk 确保所有内嵌插件在磁盘上存在。
// 插件与主题一样以磁盘目录作为运行时来源：首次启动释放内置插件；已存在的插件保留本地调整。
func ensurePluginsOnDisk() {
	entries, err := fs.ReadDir(bundledplugins.Plugins, ".")
	if err != nil {
		slog.Warn("读取内嵌插件失败", "error", err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginID := entry.Name()
		pluginYAML := filepath.Join("plugins", pluginID, "plugin.yaml")
		if _, err := os.Stat(pluginYAML); err == nil {
			continue
		}
		if err := releaseBundledPlugin(pluginID); err != nil {
			slog.Warn("释放内嵌插件失败", "plugin", pluginID, "error", err)
		}
	}
}

func releaseBundledPlugin(pluginID string) error {
	pluginDir := filepath.Join("plugins", pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(bundledplugins.Plugins, pluginID, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join("plugins", path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(bundledplugins.Plugins, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
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
	for _, enabledID := range pm.EnabledIDs(ctx) {
		if enabledID == id {
			return true
		}
	}
	return false
}
