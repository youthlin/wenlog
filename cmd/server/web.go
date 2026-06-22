package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/youthlin/blog/internal/config"
	"github.com/youthlin/blog/internal/handler"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/theme"
	"github.com/youthlin/blog/internal/util"
	"github.com/youthlin/blog/web"
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

	// 定时发布 goroutine：每分钟检查一次
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	util.Go(func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n, err := st.PublishScheduled(ctx)
				if err != nil {
					slog.Error("检查定时发布文章失败", slog.Any("error", err))
				} else if n > 0 {
					slog.Info("检查定时发布文章成功", slog.Int64("count", n))
				}
			case <-ctx.Done():
				return
			}
		}
	})
	// 自动备份 goroutine：每天凌晨 3 点执行
	util.Go(func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
			if now.After(next) {
				next = next.Add(24 * time.Hour)
			}
			d := next.Sub(now)
			slog.Info("下次定时自动备份数据库", slog.Time("at", next), slog.Duration("in", d))
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return
			}
			path, err := st.BackupDB()
			if err != nil {
				slog.Error("备份数据库失败", slog.Any("error", err))
			} else {
				slog.Info("备份数据库成功", slog.String("path", path))
				_ = st.CleanOldBackups(10)
			}
		}
	})

	util.Go(func() {
		<-quit
		slog.Info("系统正在退出...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("系统退出成功", slog.Any("error", err))
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

	// 模板与前端资源
	// 开发环境优先直接读磁盘。若存在本地模板目录,后台会显示 hot 状态并允许手动重新解析;
	// 静态资源仍然直接读磁盘即时生效。缺失时统一回退到 embed。
	tplRenderer, err := templateRenderer()
	if err != nil {
		slog.Error("加载模板失败", slog.Any("error", err))
		os.Exit(1)
	}
	r.HTMLRender = tplRenderer

	// 静态资源
	// 历史图片(原路径)+ css/js。开发环境优先直接读磁盘,避免每次改样式/JS 都重启。
	// 添加 Cache-Control 头：历史图片/上传文件 1 天，全局资源 1 天。
	r.GET("/wp-content/*filepath", cacheStatic(http.Dir(filepath.Join(cfg.PublicDir, "wp-content")), 86400))
	r.GET("/uploads/*filepath", cacheStatic(http.Dir(filepath.Join(cfg.PublicDir, "uploads")), 86400))
	assetLocalFS := assetFS()
	r.GET("/assets/*filepath", cacheStatic(assetLocalFS, 86400))

	// 监控与健康检查。/metrics 使用后台可配置密码保护,避免公网暴露指标。
	r.GET("/metrics", middleware.MetricsBasicAuth(st), gin.WrapH(promhttp.Handler()))
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// 前台
	// 确保内嵌主题在磁盘上存在（优先从 embed 释放）
	ensureThemesOnDisk()
	tm, err := theme.NewManager("themes", st)
	if err != nil {
		slog.Error("初始化主题管理器失败", slog.Any("error", err))
		os.Exit(1)
	}
	tm.SetRenderer(tplRenderer)
	// 设置默认主题 embed FS，供 ResetToDefault 时从 embed 加载
	if defaultThemeFS, err := fs.Sub(web.Themes, "themes/default/templates"); err == nil {
		tplRenderer.SetDefaultThemeFS(defaultThemeFS)
	}
	// 启动时加载当前激活主题（含 functions.go）
	if err := tm.LoadTheme(context.Background(), ""); err != nil {
		slog.Error("加载当前主题失败", slog.Any("error", err))
	}
	pub := handler.NewPublic(st, cfg, tm, tplRenderer)
	rateLimiter := middleware.NewMemoryRateLimiter()
	registerPublicRoutes(r, pub, rateLimiter)

	// 认证（独立于前台和后台）
	auth := handler.NewAuth(st)
	registerAuthRoutes(r, auth, rateLimiter)

	// 后台
	adm := handler.NewAdmin(st, cfg, tplRenderer, assetLocalFS, tm)
	registerAdminRoutes(r, adm, auth, st, rateLimiter)

	// 注入 themeWidgets 模板函数实现
	render.SetThemeWidgetsProvider(func(area string) any {
		t := tm.Current(context.Background())
		if t == nil {
			return nil
		}
		config, _ := st.GetSetting(context.Background(), "widget_"+area)
		return theme.ResolveWidgets(config, t, area)
	})

	// 注入 option 模板函数实现（themeData "option" 调用）
	render.SetOptionProvider(func(optionID string) string {
		t := tm.Current(context.Background())
		if t == nil {
			return ""
		}
		key := theme.OptionKey(t.Name, optionID)
		val, _ := st.GetSetting(context.Background(), key)
		if val == "" {
			// 回退到 theme.yaml 中的 default
			for _, opt := range t.Options {
				if opt.ID == optionID {
					return opt.Default
				}
			}
		}
		return val
	})

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
	return r
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

func assetFS() *localFirstFileSystem {
	assetsFS, err := fs.Sub(web.Assets, "assets")
	if err != nil {
		panic(err)
	}
	fsys := &localFirstFileSystem{
		dir:      "web/assets",
		fallback: http.FS(assetsFS),
	}
	if _, err := os.Stat(fsys.dir); err == nil {
		fsys.hot.Store(true)
	}
	return fsys
}

type localFirstFileSystem struct {
	dir      string
	fallback http.FileSystem
	hot      atomic.Bool
}

func (fsys *localFirstFileSystem) Open(name string) (http.File, error) {
	if fsys != nil && fsys.hot.Load() {
		return http.Dir(fsys.dir).Open(name)
	}
	return fsys.fallback.Open(name)
}

func (fsys *localFirstFileSystem) Hot() bool {
	return fsys != nil && fsys.hot.Load()
}

func (fsys *localFirstFileSystem) SetHot(v bool) {
	if fsys == nil {
		return
	}
	fsys.hot.Store(v)
}

// ensureThemesOnDisk 确保所有内嵌主题在磁盘上存在。
// 优先从 embed 释放到 themes/ 目录，已存在则跳过。
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
			continue // 已存在，跳过
		}
		_ = os.MkdirAll(filepath.Join("themes", themeName), 0o755)
		prefix := filepath.Join("themes", themeName)
		if err := fs.WalkDir(web.Themes, prefix, func(path string, d fs.DirEntry, err error) error {
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
		}); err != nil {
			slog.Warn("释放内嵌主题失败", "theme", themeName, "error", err)
		}
	}
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
