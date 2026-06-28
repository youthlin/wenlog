package main

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/handler"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/store"
)

// registerPublicRoutes 注册前台路由。
//
// 路由难点:文章永久链接 /{year}{id}.html 与页面 /{slug} 都在根路径下,
// 用单一通配路由分发,在 handler 内按格式区分。
func registerPublicRoutes(r *gin.Engine, pub *handler.Public) {
	// 首页 + /?p=ID 旧链接重定向。
	r.GET("/", func(c *gin.Context) {
		if pid := c.Query("p"); pid != "" {
			if pub.PostIDRedirect(c, pid) {
				return
			}
			pub.NotFound(c)
			return
		}
		pub.Index(c)
	})

	r.GET("/search", pub.Search)
	r.GET("/sitemap.xml", pub.Sitemap)
	r.POST("/comment", pub.SubmitComment)
	r.GET("/feed", pub.Feed)

	// 兜底动态路由: 页面 slug + 任意层级文章永久链接 + 旧链接兼容。
	r.NoRoute(pub.DynamicOrLegacy)
}

// registerAuthRoutes 注册认证路由(/auth/*),无需登录。
func registerAuthRoutes(r *gin.Engine, auth *handler.Auth, limiter middleware.RateLimiter) {
	loginLimiter := middleware.RateLimitMiddleware(limiter, middleware.RateLimitConfig{
		Window:  15 * time.Minute,
		Max:     5,
		KeyFunc: middleware.DefaultRateLimitKey,
	})
	registerLimiter := middleware.RateLimitMiddleware(limiter, middleware.RateLimitConfig{
		Window:  1 * time.Hour,
		Max:     3,
		KeyFunc: middleware.DefaultRateLimitKey,
	})
	forgotLimiter := middleware.RateLimitMiddleware(limiter, middleware.RateLimitConfig{
		Window:  15 * time.Minute,
		Max:     5,
		KeyFunc: middleware.DefaultRateLimitKey,
	})

	r.GET("/auth/login", auth.LoginForm)
	r.POST("/auth/login", loginLimiter, auth.Login)
	r.GET("/auth/register", auth.RegisterForm)
	r.POST("/auth/register", registerLimiter, auth.Register)
	r.GET("/auth/register/verify", auth.RegisterVerifyForm)
	r.POST("/auth/register/verify", auth.RegisterVerify)
	r.GET("/auth/forgot-password", auth.ForgotPasswordForm)
	r.POST("/auth/forgot-password", forgotLimiter, auth.ForgotPassword)
	r.GET("/auth/reset-password", auth.ResetPasswordForm)
	r.POST("/auth/reset-password", auth.ResetPassword)
}

// registerAdminRoutes 注册后台路由(/admin/*),除登录页外均需认证。
func registerAdminRoutes(r *gin.Engine, adm *handler.Admin, auth *handler.Auth, st *store.Store) {
	// 所有角色可访问(仅需登录)。
	g := r.Group("/admin")
	g.Use(middleware.AuthRequired(st), middleware.CSRFMiddleware())
	g.GET("/", adm.Dashboard) // 欢迎页

	r.POST("/auth/logout", middleware.AuthRequired(st), middleware.CSRFMiddleware(), auth.Logout)

	// 个人资料(所有角色可访问)。
	profileGroup := r.Group("/admin")
	profileGroup.Use(middleware.AuthRequired(st), middleware.CSRFMiddleware())
	profileGroup.GET("/profile", adm.ProfilePage)
	profileGroup.POST("/profile", adm.SaveProfileSettings)
	profileGroup.GET("/profile/email/verify", adm.VerifyProfileEmail)
	profileGroup.POST("/profile/password", adm.SavePasswordSettings)
	profileGroup.GET("/my-comments", adm.ListComments)
	profileGroup.POST("/my-comments/:id/edit", adm.EditMyComment)
	profileGroup.POST("/my-comments/:id/delete", adm.DeleteMyComment)
	profileGroup.GET("/export-data", adm.ExportDataPage)
	profileGroup.POST("/export-data", adm.ExportData)
	profileGroup.GET("/delete-account", adm.DeleteAccountPage)
	profileGroup.POST("/delete-account", adm.DeleteAccount)

	// admin + author: 内容管理。
	contentGroup := r.Group("/admin")
	contentGroup.Use(middleware.AuthRequired(st), middleware.CSRFMiddleware(),
		middleware.RequireRole(model.RoleAdmin, model.RoleAuthor))
	contentGroup.GET("/posts", adm.ListPosts)
	contentGroup.GET("/post/new", adm.EditPostForm)
	contentGroup.GET("/post/:id", adm.EditPostForm)
	contentGroup.POST("/post", adm.SavePost)
	contentGroup.POST("/post/:id/delete", adm.DeletePost)
	contentGroup.POST("/post/:id/menu-order", adm.UpdateMenuOrder)
	contentGroup.POST("/preview", adm.Preview)
	contentGroup.GET("/comments", adm.ListComments)
	contentGroup.POST("/comment/:id/:action", adm.ModerateComment)
	contentGroup.POST("/comments/edit/:id", adm.EditComment)
	contentGroup.POST("/comments/batch", adm.BatchComments)
	contentGroup.GET("/uploads", adm.UploadsPage)
	contentGroup.GET("/uploads.json", adm.UploadsJSON)
	contentGroup.POST("/upload", adm.UploadFile)
	contentGroup.POST("/upload/:id/delete", adm.DeleteUpload)
	contentGroup.GET("/post/:id/revisions", adm.RevisionsPage)
	contentGroup.GET("/post/:id/revision/:revId", adm.RevisionView)
	contentGroup.POST("/post/:id/revision/:revId/restore", adm.RevisionRestore)

	// admin only: 设置、工具、用户管理。
	adminGroup := r.Group("/admin")
	adminGroup.Use(middleware.AuthRequired(st), middleware.CSRFMiddleware(),
		middleware.RequireRole(model.RoleAdmin))
	adminGroup.GET("/settings", adm.SettingsPage)
	adminGroup.GET("/settings/developer", adm.DeveloperSettingsPage)
	adminGroup.POST("/settings/site", adm.SaveSiteSettings)
	adminGroup.POST("/settings/smtp", adm.SaveSMTPSettings)
	adminGroup.POST("/settings/smtp/test", adm.TestSMTPSettings)
	adminGroup.POST("/settings/session", adm.SaveSessionSettings)
	adminGroup.POST("/settings/metrics", adm.SaveMetricsAuthSettings)
	adminGroup.POST("/settings/sql-details", adm.SaveSQLDetailsSettings)
	adminGroup.POST("/settings/assets/release", adm.ReleaseAssets)
	adminGroup.POST("/settings/assets/embed", adm.UseEmbeddedAssets)
	adminGroup.POST("/settings/i18n/release", adm.ReleaseI18n)
	adminGroup.POST("/settings/i18n/embed", adm.UseEmbeddedI18n)
	adminGroup.POST("/settings/templates/release", adm.ReleaseTemplates)
	adminGroup.POST("/settings/templates/embed", adm.UseEmbeddedTemplates)
	adminGroup.POST("/settings/templates/reload", adm.ReloadTemplates)
	adminGroup.POST("/settings/theme/reload", adm.ReloadTheme)
	adminGroup.POST("/settings/plugins/reload", adm.ReloadPlugins)
	adminGroup.GET("/debug", adm.DebugPage)
	adminGroup.POST("/debug", adm.DebugPage)
	adminGroup.GET("/terms", adm.TermsPage)
	adminGroup.GET("/categories", adm.CategoriesPage)
	adminGroup.GET("/tags", adm.TagsPage)
	adminGroup.POST("/category", adm.SaveCategory)
	adminGroup.POST("/category/:id/delete", adm.DeleteCategory)
	adminGroup.POST("/tag", adm.SaveTag)
	adminGroup.POST("/tag/:id/delete", adm.DeleteTag)
	adminGroup.GET("/import", adm.ImportPage)
	adminGroup.POST("/import", adm.ImportXML)
	adminGroup.POST("/export", adm.ExportXML)
	adminGroup.GET("/users", adm.ListUsers)
	adminGroup.GET("/user/new", adm.NewUserForm)
	adminGroup.POST("/user/new", adm.CreateUser)
	adminGroup.GET("/user/:id/edit", adm.EditUserForm)
	adminGroup.POST("/user/:id/edit", adm.UpdateUser)
	adminGroup.POST("/user/:id/role", adm.UpdateUserRole)
	adminGroup.POST("/user/:id/delete", adm.DeleteUser)

	// 主题管理
	adminGroup.GET("/themes", adm.ThemesPage)
	adminGroup.POST("/theme/upload", adm.ThemeUpload)
	adminGroup.POST("/theme/activate", adm.ThemeActivate)
	adminGroup.POST("/theme/delete", adm.ThemeDelete)
	adminGroup.GET("/theme/download", adm.ThemeDownload)
	adminGroup.POST("/theme/preview", adm.ThemePreview)
	adminGroup.POST("/theme/preview/clear", adm.ThemePreviewClear)
	adminGroup.GET("/theme/screenshot/:name/:file", adm.ThemeScreenshot)

	// 主题文件编辑器
	adminGroup.GET("/theme/files", adm.ThemeFilesPage)
	adminGroup.GET("/theme/file", adm.ThemeFileRead)
	adminGroup.POST("/theme/file", adm.ThemeFileSave)
	adminGroup.POST("/theme/file/create", adm.ThemeFileCreate)
	adminGroup.POST("/theme/file/delete", adm.ThemeFileDelete)
	adminGroup.POST("/theme/recovery/clear", adm.ThemeRecoveryClear)
	adminGroup.POST("/theme/reload", adm.ThemeReload)

	// 数据库备份
	adminGroup.GET("/backup", adm.BackupPage)
	adminGroup.POST("/backup", adm.BackupNow)
	adminGroup.POST("/backup/settings", adm.SaveBackupSettings)
	adminGroup.POST("/backup/email", adm.BackupEmail)
	adminGroup.POST("/backup/restore", adm.BackupRestore)
	adminGroup.POST("/backup/delete", adm.BackupDelete)

	// 组件管理
	adminGroup.GET("/widgets", adm.WidgetsPage)
	adminGroup.POST("/widgets", adm.SaveWidgets)
	adminGroup.GET("/menus", adm.MenusPage)
	adminGroup.POST("/menus", adm.SaveMenus)

	// 插件管理
	adminGroup.GET("/plugins", adm.PluginsPage)
	adminGroup.POST("/plugin/:id/:action", adm.PluginAction)
	adminGroup.GET("/plugin/:id/settings", adm.PluginSettingsPage)
	adminGroup.POST("/plugin/:id/settings", adm.SavePluginSettings)

	// 主题全局选项
	adminGroup.GET("/theme-options", adm.ThemeOptionsPage)
	adminGroup.POST("/theme-options", adm.SaveThemeOptions)
}
