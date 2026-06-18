package main

import (
	"context"
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
	"github.com/youthlin/blog/web"
)

// serve 启动 web 服务器
func serve(cfg *config.Config, log *slog.Logger, st *store.Store) error {
	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: createWebHandler(cfg, log, st),
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	go func() {
		<-quit
		log.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("shutdown", slog.Any("error", err))
		}
	}()
	log.Info("server listening", slog.String("addr", cfg.Addr))
	err := srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// createWebHandler 创建并注册路由。
func createWebHandler(cfg *config.Config, log *slog.Logger, st *store.Store) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.ContextWithFallback = true

	// 中间件
	r.Use(
		middleware.Recover(log),
		middleware.TraceID(),
		middleware.Logger(log),
		middleware.Metrics(),
		middleware.SQLTracer(log),
		middleware.Session(log, st)(),
		i18n.Middleware(),
	)

	// 模板与前端资源
	// 开发环境优先直接读磁盘。若存在本地模板目录,后台会显示 hot 状态并允许手动重新解析;
	// 静态资源仍然直接读磁盘即时生效。缺失时统一回退到 embed。
	tplRenderer, err := templateRenderer()
	if err != nil {
		log.Error("load templates", slog.Any("error", err))
		os.Exit(1)
	}
	r.HTMLRender = tplRenderer

	// 静态资源
	// 历史图片(原路径)+ css/js。开发环境优先直接读磁盘,避免每次改样式/JS 都重启。
	r.Static("/wp-content", filepath.Join(cfg.PublicDir, "wp-content"))
	r.Static("/uploads", filepath.Join(cfg.PublicDir, "uploads"))
	assetLocalFS := assetFS()
	r.StaticFS("/assets", assetLocalFS)

	// 监控与健康检查。/metrics 使用后台可配置密码保护,避免公网暴露指标。
	r.GET("/metrics", middleware.MetricsBasicAuth(st), gin.WrapH(promhttp.Handler()))
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// 前台
	tm, err := theme.NewManager("themes", st)
	if err != nil {
		log.Error("init theme manager", slog.Any("error", err))
		os.Exit(1)
	}
	// 启动时加载当前激活主题的模板
	if current := tm.Current(context.Background()); current != nil && current.HasTemplates() {
		if err := tplRenderer.LoadTheme(current.TemplatesDir()); err != nil {
			log.Error("load current theme templates", slog.Any("error", err))
		}
	}
	pub := handler.NewPublic(st, cfg, log, tm)
	rateLimiter := middleware.NewMemoryRateLimiter()
	registerPublicRoutes(r, pub, rateLimiter)

	// 后台
	adm := handler.NewAdmin(st, cfg, log, tplRenderer, assetLocalFS, tm)
	registerAdminRoutes(r, adm, st, rateLimiter)

	// 当前主题静态资源：/theme-assets/... → themes/{current}/assets/...
	r.GET("/theme-assets/*filepath", func(c *gin.Context) {
		current := tm.Current(c)
		if current == nil || !current.HasAssets() {
			c.Status(http.StatusNotFound)
			return
		}
		name := strings.TrimPrefix(filepath.Clean(c.Param("filepath")), string(filepath.Separator))
		if name == "." || strings.HasPrefix(name, "..") {
			c.Status(http.StatusNotFound)
			return
		}
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
