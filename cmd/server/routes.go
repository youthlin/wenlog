package main

import (
	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/handler"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
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

	// 前台用户路由(需登录,所有角色可访问)。
	userGroup := r.Group("")
	userGroup.Use(middleware.AuthRequiredRedirect("/login"))
	userGroup.GET("/profile", pub.ProfilePage)
	userGroup.POST("/profile", pub.SaveProfile)
	userGroup.POST("/profile/password", pub.ChangePassword)
	userGroup.GET("/my-comments", pub.MyComments)
	userGroup.POST("/my-comments/:id/delete", pub.DeleteMyComment)
	userGroup.GET("/export-data", pub.ExportData)
	userGroup.POST("/delete-account", pub.DeleteAccount)

	// 前台认证路由(无需登录)。
	r.GET("/register", pub.RegisterForm)
	r.POST("/register", pub.Register)
	r.GET("/login", pub.LoginForm)
	r.POST("/login", pub.Login)
	r.POST("/logout", pub.Logout)

	// 兜底动态路由: 页面 slug + 任意层级文章永久链接 + 旧链接兼容。
	r.NoRoute(pub.DynamicOrLegacy)
}

// registerAdminRoutes 注册后台路由(/admin/*),除登录页外均需认证。
func registerAdminRoutes(r *gin.Engine, adm *handler.Admin) {
	r.GET("/admin/login", adm.LoginForm)
	r.POST("/admin/login", adm.Login)

	// 所有角色可访问(仅需登录)。
	g := r.Group("/admin")
	g.Use(middleware.AuthRequired(), middleware.CSRFMiddleware())
	g.GET("/", adm.Dashboard)
	g.POST("/logout", adm.Logout)

	// admin + author: 内容管理。
	contentGroup := r.Group("/admin")
	contentGroup.Use(middleware.AuthRequired(), middleware.CSRFMiddleware(),
		middleware.RequireRole(model.RoleAdmin, model.RoleAuthor))
	contentGroup.GET("/posts", adm.ListPosts)
	contentGroup.GET("/post/new", adm.EditPostForm)
	contentGroup.GET("/post/:id", adm.EditPostForm)
	contentGroup.POST("/post", adm.SavePost)
	contentGroup.POST("/post/:id/delete", adm.DeletePost)
	contentGroup.POST("/preview", adm.Preview)
	contentGroup.GET("/comments", adm.ListComments)
	contentGroup.POST("/comment/:id/:action", adm.ModerateComment)
	contentGroup.POST("/comments/edit/:id", adm.EditComment)
	contentGroup.POST("/comments/batch", adm.BatchComments)
	contentGroup.GET("/categories", adm.CategoriesPage)
	contentGroup.GET("/tags", adm.TagsPage)
	contentGroup.POST("/category", adm.SaveCategory)
	contentGroup.POST("/category/:id/delete", adm.DeleteCategory)
	contentGroup.POST("/tag", adm.SaveTag)
	contentGroup.POST("/tag/:id/delete", adm.DeleteTag)
	contentGroup.GET("/uploads", adm.UploadsPage)
	contentGroup.GET("/uploads.json", adm.UploadsJSON)
	contentGroup.POST("/upload", adm.UploadFile)
	contentGroup.POST("/upload/:id/delete", adm.DeleteUpload)

	// admin only: 设置、工具、用户管理。
	adminGroup := r.Group("/admin")
	adminGroup.Use(middleware.AuthRequired(), middleware.CSRFMiddleware(),
		middleware.RequireRole(model.RoleAdmin))
	adminGroup.GET("/settings", adm.SettingsPage)
	adminGroup.GET("/settings/user", adm.UserSettingsPage)
	adminGroup.GET("/settings/developer", adm.DeveloperSettingsPage)
	adminGroup.POST("/settings/site", adm.SaveSiteSettings)
	adminGroup.POST("/settings/session", adm.SaveSessionSettings)
	adminGroup.POST("/settings/assets/release", adm.ReleaseAssets)
	adminGroup.POST("/settings/assets/embed", adm.UseEmbeddedAssets)
	adminGroup.POST("/settings/i18n/release", adm.ReleaseI18n)
	adminGroup.POST("/settings/i18n/embed", adm.UseEmbeddedI18n)
	adminGroup.POST("/settings/templates/release", adm.ReleaseTemplates)
	adminGroup.POST("/settings/templates/embed", adm.UseEmbeddedTemplates)
	adminGroup.POST("/settings/templates/reload", adm.ReloadTemplates)
	adminGroup.POST("/settings/profile", adm.SaveProfileSettings)
	adminGroup.POST("/settings/password", adm.SavePasswordSettings)
	adminGroup.GET("/debug", adm.DebugPage)
	adminGroup.POST("/debug", adm.DebugPage)
	adminGroup.GET("/terms", adm.TermsPage)
	adminGroup.GET("/import", adm.ImportPage)
	adminGroup.POST("/import", adm.ImportXML)
	adminGroup.POST("/export", adm.ExportXML)
	adminGroup.GET("/users", adm.ListUsers)
	adminGroup.POST("/user/:id/role", adm.UpdateUserRole)
	adminGroup.POST("/user/:id/delete", adm.DeleteUser)
}
