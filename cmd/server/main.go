// Command server 启动博客 HTTP 服务。
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gomarkdown/markdown"
	mhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/blog/internal/config"
	"github.com/youthlin/blog/internal/handler"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"
	"github.com/youthlin/blog/web"
)

var (
	setUser = flag.String("set-admin", "", "设置后台管理员密码:格式 username[:password],密码不填会自动生成并打印,设置后退出")
)

func main() {
	// 解析命令行参数
	flag.Parse()

	// 初始化
	var (
		cfg     = config.Load()
		log     = newLogger(cfg.LogJSON)
		st, err = store.Open(cfg.DBPath)
	)
	if err != nil {
		log.Error("open store", slog.Any("error", err))
		os.Exit(1)
	}

	// 重置密码
	if setPasswd(st) {
		return
	}

	// 自动创建管理员
	if err = ensureInitialAdmin(st); err != nil {
		log.Error("ensure initial admin", slog.Any("error", err))
		os.Exit(1)
	}
	if err = ensureInitialContent(st); err != nil {
		log.Error("ensure initial content", slog.Any("error", err))
		os.Exit(1)
	}
	if err = i18n.Init(); err != nil {
		log.Error("init i18n", slog.Any("error", err))
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: createWebHandler(cfg, log, st),
	}
	go func() {
		log.Info("server listening", slog.String("addr", cfg.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("listen", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("shutdown", slog.Any("error", err))
	}
}

func newLogger(jsonOut bool) *slog.Logger {
	opts := &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}
	var h slog.Handler
	if jsonOut {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// setPasswd 如果有 -set-admin 参数 执行密码重置 并退出
func setPasswd(st *store.Store) bool {
	spec := *setUser
	if spec == "" { // 没有传该参数
		return false
	}
	username, password, ok := strings.Cut(spec, ":")
	if !ok {
		username = spec
		password = util.GenerateRandomString(8)
		fmt.Printf("已生成随机密码: %s\n", password)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "密码处理失败, err=%v\n", err)
		os.Exit(1)
	}
	err = st.UpsertUserPassword(username, username, string(hash))
	if err != nil {
		fmt.Fprintf(os.Stderr, "设置密码失败, err=%v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已为用户 %s 重置密码\n", username)
	return true
}

// ensureInitialAdmin 启动时如果没有用户, 自动创建 admin 用户
func ensureInitialAdmin(st *store.Store) error {
	n, err := st.CountUsers()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	password := util.GenerateRandomString(12)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := st.UpsertUserPassword("admin", "admin", string(hash)); err != nil {
		return err
	}
	fmt.Printf("已自动创建管理员, 用户名: admin 密码: %s\n", password)
	return nil
}

// ensureInitialContent 启动时如果还没有任何内容, 自动插入欢迎文章、示例评论和关于页面。
func ensureInitialContent(st *store.Store) error {
	total, err := st.CountPosts()
	if err != nil {
		return err
	}
	if total > 0 {
		return nil
	}
	author, err := st.GetUserByUsername("admin")
	if err != nil {
		users, listErr := st.ListUsers()
		if listErr != nil {
			return listErr
		}
		if len(users) == 0 {
			return fmt.Errorf("初始化内容失败: 未找到可用作者")
		}
		author = &users[0]
	}
	uncategorized := &model.Category{
		Name: "未分类",
		Slug: "uncategorized",
	}
	if err := st.SaveCategory(uncategorized); err != nil {
		return err
	}
	now := time.Now()
	postID, err := st.NextPostID()
	if err != nil {
		return err
	}
	welcomeMD := strings.TrimSpace(`欢迎使用这个独立博客程序。它专注于：

- **简单部署**：单二进制 + SQLite，适合个人博客快速上线。
- **内容优先**：支持 Markdown 写作，也兼容逐步整理已有内容。
- **可持续维护**：后台可管理文章、页面、评论、资源与模板。

你可以先到后台看看设置、写一篇文章、再把这里改成真正属于你自己的首页开场白。祝写作愉快。`)
	welcome := &model.Post{
		ID:            postID,
		Title:         "欢迎来到我的博客",
		ContentMD:     welcomeMD,
		Content:       renderMarkdownForBootstrap(welcomeMD),
		AuthorID:      author.ID,
		Status:        model.StatusPublished,
		PostType:      model.PostTypePost,
		ContentFormat: model.FormatMarkdown,
		CommentStatus: "open",
		PublishedAt:   now,
		ModifiedAt:    now,
	}
	if err := st.SavePostWithTerms(welcome, []uint{uncategorized.ID}, nil); err != nil {
		return err
	}
	comment := &model.Comment{
		PostID:    welcome.ID,
		Author:    "youthlin",
		Email:     "youthlinchen@outlook.com",
		URL:       "https://github.com/youthlin/blog",
		IP:        "127.0.0.1",
		Content:   "欢迎使用这个博客程序，你可以在后台管理评论，快去看看吧。",
		Status:    model.CommentApproved,
		CreatedAt: now.Add(time.Minute),
		ParentID:  0,
	}
	if err := st.CreateComment(comment); err != nil {
		return err
	}
	aboutID, err := st.NextPostID()
	if err != nil {
		return err
	}
	aboutMD := strings.TrimSpace(`你好，欢迎来到这里。

我是这个博客的作者，喜欢记录技术、写作、日常想法，也会把一些正在尝试和思考的东西慢慢整理出来。这里不会只放“结论”，也会保留过程、踩坑和一些还没完全想清楚的问题。

如果你恰好读到某篇文章，希望它能给你一点启发；如果你也在做相似的事情，欢迎交流。`)
	about := &model.Post{
		ID:            aboutID,
		Title:         "关于",
		Slug:          "about",
		ContentMD:     aboutMD,
		Content:       renderMarkdownForBootstrap(aboutMD),
		AuthorID:      author.ID,
		Status:        model.StatusPublished,
		PostType:      model.PostTypePage,
		ContentFormat: model.FormatMarkdown,
		CommentStatus: "closed",
		MenuOrder:     1,
		PublishedAt:   now,
		ModifiedAt:    now,
	}
	return st.SavePost(about)
}

func renderMarkdownForBootstrap(md string) string {
	p := parser.NewWithExtensions(parser.CommonExtensions | parser.AutoHeadingIDs)
	doc := p.Parse([]byte(md))
	rendererMD := mhtml.NewRenderer(mhtml.RendererOptions{Flags: mhtml.CommonFlags})
	out := string(markdown.Render(doc, rendererMD))
	return render.HighlightCodeBlocks(out)
}

// createWebHandler 创建并注册路由
func createWebHandler(cfg *config.Config, log *slog.Logger, st *store.Store) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// 中间件
	sessionStore, err := middleware.NewDynamicCookieStore(st)
	if err != nil {
		log.Error("init session store", slog.Any("error", err))
		os.Exit(1)
	}
	sessionStore.Options(sessions.Options{
		Path:     "/",
		HttpOnly: true,
		MaxAge:   7 * 86400,
		SameSite: http.SameSiteLaxMode,
	})
	r.Use(
		middleware.Recover(log),
		middleware.Logger(log),
		middleware.Metrics(),
		sessions.Sessions("blog_session", sessionStore),
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
	r.StaticFS("/assets", assetFS())

	// 监控与健康检查
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// 前台
	pub := handler.NewPublic(st, cfg, log)
	registerPublicRoutes(r, pub)

	// 后台
	adm := handler.NewAdmin(st, cfg, log, tplRenderer)
	registerAdminRoutes(r, adm)
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

func assetFS() http.FileSystem {
	assetsFS, err := fs.Sub(web.Assets, "assets")
	if err != nil {
		panic(err)
	}
	return localFirstFileSystem{
		dir:      "web/assets",
		fallback: http.FS(assetsFS),
	}
}

type localFirstFileSystem struct {
	dir      string
	fallback http.FileSystem
}

func (fsys localFirstFileSystem) Open(name string) (http.File, error) {
	if _, err := os.Stat(fsys.dir); err == nil {
		return http.Dir(fsys.dir).Open(name)
	}
	return fsys.fallback.Open(name)
}

// registerPublicRoutes 注册前台路由。
//
// 路由难点:文章永久链接 /{year}{id}.html 与页面 /{slug} 都在根路径下,
// 用单一通配路由分发,在 handler 内按格式区分。
func registerPublicRoutes(r *gin.Engine, pub *handler.Public) {
	// 首页 + /?p=ID 旧链接重定向。
	r.GET("/", func(c *gin.Context) {
		if c.Query("p") != "" {
			if pub.LegacyQueryRedirect(c) {
				return
			}
			pub.NotFoundOrLegacy(c)
			return
		}
		pub.Index(c)
	})

	r.GET("/category/:slug", pub.Category)
	r.GET("/tag/:slug", pub.Tag)
	r.GET("/search", pub.Search)
	r.POST("/comment", pub.SubmitComment)
	r.GET("/feed", pub.Feed)

	// 根路径下的单段路由:既可能是文章永久链接,也可能是页面 slug。
	r.GET("/:seg", func(c *gin.Context) {
		path := c.Request.URL.Path
		if _, _, ok := permalink.ParsePostPath(path); ok {
			pub.Post(c)
			return
		}
		// 否则当作页面 slug。
		c.Params = append(c.Params, gin.Param{Key: "slug", Value: c.Param("seg")})
		pub.Page(c)
	})

	// 兜底:旧 slug 301 或 404。
	r.NoRoute(pub.NotFoundOrLegacy)
}

// registerAdminRoutes 注册后台路由(/admin/*),除登录页外均需认证。
func registerAdminRoutes(r *gin.Engine, adm *handler.Admin) {
	r.GET("/admin/login", adm.LoginForm)
	r.POST("/admin/login", adm.Login)

	g := r.Group("/admin")
	g.Use(middleware.AuthRequired(), middleware.CSRFMiddleware())
	g.GET("/", adm.Dashboard)
	g.POST("/logout", adm.Logout)
	g.GET("/settings", adm.SettingsPage)
	g.POST("/settings/site", adm.SaveSiteSettings)
	g.POST("/settings/session", adm.SaveSessionSettings)
	g.POST("/settings/assets/release", adm.ReleaseAssets)
	g.POST("/settings/templates/release", adm.ReleaseTemplates)
	g.POST("/settings/templates/reload", adm.ReloadTemplates)
	g.POST("/settings/profile", adm.SaveProfileSettings)
	g.POST("/settings/password", adm.SavePasswordSettings)
	g.GET("/debug", adm.DebugPage)
	g.POST("/debug", adm.DebugPage)
	g.GET("/terms", adm.TermsPage)
	g.POST("/category", adm.SaveCategory)
	g.POST("/category/:id/delete", adm.DeleteCategory)
	g.POST("/tag", adm.SaveTag)
	g.POST("/tag/:id/delete", adm.DeleteTag)
	g.GET("/uploads", adm.UploadsPage)
	g.GET("/uploads.json", adm.UploadsJSON)
	g.POST("/upload", adm.UploadFile)
	g.POST("/upload/:id/delete", adm.DeleteUpload)
	g.GET("/import", adm.ImportPage)
	g.POST("/import", adm.ImportXML)
	g.POST("/export", adm.ExportXML)

	g.GET("/posts", adm.ListPosts)
	g.GET("/post/new", adm.EditPostForm)
	g.GET("/post/:id", adm.EditPostForm)
	g.POST("/post", adm.SavePost)
	g.POST("/post/:id/delete", adm.DeletePost)
	g.POST("/preview", adm.Preview)

	g.GET("/comments", adm.ListComments)
	g.POST("/comment/:id/:action", adm.ModerateComment)
	g.POST("/comments/edit/:id", adm.EditComment)
	g.POST("/comments/batch", adm.BatchComments)

	g.GET("/links", adm.ListLinks)
	g.POST("/link", adm.SaveLink)
	g.POST("/link/:id/delete", adm.DeleteLink)

}
