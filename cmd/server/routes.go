package main

import (
	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/handler"
	"github.com/youthlin/blog/internal/middleware"
)

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

	r.GET("/search", pub.Search)
	r.POST("/comment", pub.SubmitComment)
	r.GET("/feed", pub.Feed)

	// 兜底动态路由: 页面 slug + 任意层级文章永久链接 + 旧链接兼容。
	r.NoRoute(pub.DynamicOrLegacy)
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
	g.GET("/settings/user", adm.UserSettingsPage)
	g.GET("/settings/developer", adm.DeveloperSettingsPage)
	g.POST("/settings/site", adm.SaveSiteSettings)
	g.POST("/settings/session", adm.SaveSessionSettings)
	g.POST("/settings/assets/release", adm.ReleaseAssets)
	g.POST("/settings/assets/embed", adm.UseEmbeddedAssets)
	g.POST("/settings/i18n/release", adm.ReleaseI18n)
	g.POST("/settings/i18n/embed", adm.UseEmbeddedI18n)
	g.POST("/settings/templates/release", adm.ReleaseTemplates)
	g.POST("/settings/templates/embed", adm.UseEmbeddedTemplates)
	g.POST("/settings/templates/reload", adm.ReloadTemplates)
	g.POST("/settings/profile", adm.SaveProfileSettings)
	g.POST("/settings/password", adm.SavePasswordSettings)
	g.GET("/debug", adm.DebugPage)
	g.POST("/debug", adm.DebugPage)
	g.GET("/terms", adm.TermsPage)
	g.GET("/categories", adm.CategoriesPage)
	g.GET("/tags", adm.TagsPage)
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
}
