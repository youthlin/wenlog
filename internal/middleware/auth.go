// Package middleware — 后台认证。
package middleware

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// SessionUserKey 是 session 中存储用户 ID 的键。
const SessionUserKey = "uid"

// SessionRoleKey 是 session 中存储用户角色的键。
const SessionRoleKey = "role"

// AuthRequired 保护 /admin:未登录跳转登录页。
func AuthRequired() gin.HandlerFunc {
	return AuthRequiredRedirect("/admin/login")
}

// AuthRequiredRedirect 未登录时跳转到指定路径。
func AuthRequiredRedirect(redirectPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		s := sessions.Default(c)
		if s.Get(SessionUserKey) == nil {
			c.Redirect(http.StatusSeeOther, redirectPath)
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireRole 检查当前用户是否拥有指定角色之一。需在 AuthRequired 之后使用。
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		s := sessions.Default(c)
		role, _ := s.Get(SessionRoleKey).(string)
		for _, r := range roles {
			if role == r {
				c.Next()
				return
			}
		}
		c.String(http.StatusForbidden, "Forbidden")
		c.Abort()
	}
}

// SetSessionUser 登录时将用户 ID 和角色写入 session。
func SetSessionUser(c *gin.Context, userID uint, role string) {
	s := sessions.Default(c)
	s.Set(SessionUserKey, userID)
	s.Set(SessionRoleKey, role)
	_ = s.Save()
}

// ClearSession 清除 session(登出)。
func ClearSession(c *gin.Context) {
	s := sessions.Default(c)
	s.Clear()
	_ = s.Save()
}
