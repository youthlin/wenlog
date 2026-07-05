// Package middleware — 后台认证。
package middleware

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/youthlin/wenlog/internal/store"
)

// SessionUserKey 是 session 中存储用户 ID 的键。
const SessionUserKey = "uid"

// SessionRoleKey 是 session 中存储用户角色的键。
const SessionRoleKey = "role"

// SessionVersionKey 是 session 中存储用户会话版本的键。
const SessionVersionKey = "session_version"

// SessionPreviewThemeKey 是 session 中存储管理员预览主题名的键。
const SessionPreviewThemeKey = "preview_theme"

// AuthRequired 保护 /admin:未登录跳转登录页。
func AuthRequired(st ...*store.Store) gin.HandlerFunc {
	return AuthRequiredRedirect("/auth/login", st...)
}

// AuthRequiredRedirect 未登录时跳转到指定路径。
func AuthRequiredRedirect(redirectPath string, stores ...*store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		s := sessions.Default(c)
		uid := sessionUserID(s)
		if uid == 0 {
			c.Redirect(http.StatusSeeOther, redirectPath)
			c.Abort()
			return
		}
		if len(stores) > 0 && stores[0] != nil {
			u, err := stores[0].GetUserByID(c.Request.Context(), uid)
			if err != nil || u == nil || sessionInt64(s.Get(SessionVersionKey)) != u.SessionVersion {
				s.Clear()
				_ = s.Save()
				c.Redirect(http.StatusSeeOther, redirectPath)
				c.Abort()
				return
			}
			s.Set(SessionRoleKey, u.Role)
			c.Set("currentUser", u) // 缓存用户对象，避免 handler 层重复查库
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
func SetSessionUser(c *gin.Context, userID uint, role string, sessionVersion ...int64) {
	s := sessions.Default(c)
	s.Set(SessionUserKey, userID)
	s.Set(SessionRoleKey, role)
	if len(sessionVersion) > 0 {
		s.Set(SessionVersionKey, sessionVersion[0])
	} else {
		s.Set(SessionVersionKey, int64(0))
	}
	_ = s.Save()
}

// ClearSession 清除 session(登出)。
func ClearSession(c *gin.Context) {
	s := sessions.Default(c)
	s.Clear()
	_ = s.Save()
}

func sessionUserID(s sessions.Session) uint {
	if s == nil {
		return 0
	}
	if v := s.Get(SessionUserKey); v != nil {
		switch n := v.(type) {
		case uint:
			return n
		case int:
			return uint(n)
		case int64:
			return uint(n)
		case float64:
			return uint(n)
		}
	}
	return 0
}

// SetPreviewTheme 将预览主题名写入 session（仅管理员可用）。
func SetPreviewTheme(c *gin.Context, themeName string) {
	s := sessions.Default(c)
	s.Set(SessionPreviewThemeKey, themeName)
	_ = s.Save()
}

// ClearPreviewTheme 清除 session 中的预览主题。
func ClearPreviewTheme(c *gin.Context) {
	s := sessions.Default(c)
	s.Delete(SessionPreviewThemeKey)
	_ = s.Save()
}

// GetPreviewTheme 从 session 读取预览主题名，无预览时返回空字符串。
func GetPreviewTheme(c *gin.Context) string {
	s := sessions.Default(c)
	if v := s.Get(SessionPreviewThemeKey); v != nil {
		if name, ok := v.(string); ok && name != "" {
			return name
		}
	}
	return ""
}

func sessionInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case uint:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}
