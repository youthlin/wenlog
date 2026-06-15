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
func registerPublicRoutes(r *gin.Engine, pub *handler.Public, limiter middleware.RateLimiter) {
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

	// 前台认证路由(无需登录),带频率限制。
	forgotLimiter := middleware.RateLimitMiddleware(limiter, middleware.RateLimitConfig{
		Window:  15 * time.Minute,
		Max:     5,
		KeyFunc: middleware.DefaultRateLimitKey,
	})
	r.POST("/forgot-password", forgotLimiter, pub.ForgotPassword)

	r.GET("/forgot-password", pub.ForgotPasswordForm)
	r.GET("/reset-password", pub.ResetPasswordForm)
	r.POST("/reset-password", pub.ResetPassword)

	// 兜底动态路由: 页面 slug + 任意层级文章永久链接 + 旧链接兼容。
	r.NoRoute(pub.DynamicOrLegacy)
}

// registerAdminRoutes 注册后台路由(/admin/*),除登录页外均需认证。
func registerAdminRoutes(r *gin.Engine, adm *handler.Admin, st *store.Store, limiter middleware.RateLimiter) {
	// 登录/注册接口带频率限制,防止暴力破解。
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

	r.GET("/admin/login", adm.LoginForm)
	r.POST("/admin/login", loginLimiter, adm.Login)
	r.GET("/admin/register", adm.RegisterForm)
	r.POST("/admin/register", registerLimiter, adm.Register)
	r.GET("/admin/register/verify", adm.RegisterVerifyForm)
	r.POST("/admin/register/verify", adm.RegisterVerify)

	// 所有角色可访问(仅需登录)。
	g := r.Group("/admin")
	g.Use(middleware.AuthRequired(st), middleware.CSRFMiddleware())
	g.GET("/", adm.Dashboard) // 欢迎页
	g.POST("/logout", adm.Logout)

	// 个人资料(所有角色可访问)。
	profileGroup := r.Group("/admin")
	profileGroup.Use(middleware.AuthRequired(st), middleware.CSRFMiddleware())
	profileGroup.GET("/profile", adm.ProfilePage)
	profileGroup.POST("/profile", adm.SaveProfileSettings)
	profileGroup.GET("/profile/email/verify", adm.VerifyProfileEmail)
	profileGroup.POST("/profile/password", adm.SavePasswordSettings)
	profileGroup.GET("/my-comments", adm.ListComments)
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
	contentGroup.POST("/preview", adm.Preview)
	contentGroup.GET("/comments", adm.ListComments)
	contentGroup.POST("/comment/:id/:action", adm.ModerateComment)
	contentGroup.POST("/comments/edit/:id", adm.EditComment)
	contentGroup.POST("/comments/batch", adm.BatchComments)
	contentGroup.GET("/uploads", adm.UploadsPage)
	contentGroup.GET("/uploads.json", adm.UploadsJSON)
	contentGroup.POST("/upload", adm.UploadFile)
	contentGroup.POST("/upload/:id/delete", adm.DeleteUpload)

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
	adminGroup.POST("/settings/assets/release", adm.ReleaseAssets)
	adminGroup.POST("/settings/assets/embed", adm.UseEmbeddedAssets)
	adminGroup.POST("/settings/i18n/release", adm.ReleaseI18n)
	adminGroup.POST("/settings/i18n/embed", adm.UseEmbeddedI18n)
	adminGroup.POST("/settings/templates/release", adm.ReleaseTemplates)
	adminGroup.POST("/settings/templates/embed", adm.UseEmbeddedTemplates)
	adminGroup.POST("/settings/templates/reload", adm.ReloadTemplates)
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
}
