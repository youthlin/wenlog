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
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/youthlin/wenlog/internal/config"
	"github.com/youthlin/wenlog/internal/handler"
	"github.com/youthlin/wenlog/internal/i18n"
	"github.com/youthlin/wenlog/internal/middleware"
	"github.com/youthlin/wenlog/internal/render"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/util"
	"github.com/youthlin/wenlog/web"
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

	// 上传目录使用 wp-content, 兼容导入的数据
	wpContentFsys := http.Dir(filepath.Join(cfg.PublicDir, "wp-content"))
	r.GET("/wp-content/*filepath", cacheStatic(wpContentFsys, 86400))

	// 前端资源(admin.css/js, comment.js, common.js, theme.js)
	assets := assetFS()
	r.GET("/assets/*filepath", cacheStatic(assets, 86400))

	// 模板与前端资源
	// 开发环境优先直接读磁盘。若存在本地模板目录,后台会显示 hot 状态并允许手动重新解析;
	// 静态资源仍然直接读磁盘即时生效。缺失时统一回退到 embed。
	renderer, err := templateRenderer()
	if err != nil {
		slog.Error("加载模板失败", slog.Any("error", err))
		os.Exit(1)
	}
	r.HTMLRender = renderer

	// 加载主题、插件
	tm, pm := initHook(r, st, renderer)

	// 前台
	pub := handler.NewPublic(st, cfg, tm, renderer)
	registerPublicRoutes(r, pub)

	// 认证（独立于前台和后台）
	auth := handler.NewAuth(st)
	registerAuthRoutes(r, auth)

	// 后台
	adm := handler.NewAdmin(st, cfg, renderer, assets, tm, pm)
	registerAdminRoutes(r, adm)
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
