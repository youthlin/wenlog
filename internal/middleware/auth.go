// Package middleware — 后台认证。
package middleware

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// SessionUserKey 是 session 中存储用户 ID 的键。
const SessionUserKey = "uid"

// AuthRequired 保护 /admin:未登录跳转登录页。
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		s := sessions.Default(c)
		if s.Get(SessionUserKey) == nil {
			c.Redirect(http.StatusSeeOther, "/admin/login")
			c.Abort()
			return
		}
		c.Next()
	}
}
