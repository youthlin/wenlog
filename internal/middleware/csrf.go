package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	csrfSessionKey = "csrf_token"
	csrfContextKey = "csrf_token"
	csrfFormKey    = "_csrf"
	csrfHeaderKey  = "X-CSRF-Token"
)

// EnsureCSRFToken 确保当前会话存在 CSRF token,并返回该 token。
func EnsureCSRFToken(c *gin.Context) (string, error) {
	if c == nil {
		return "", nil
	}
	s := sessions.Default(c)
	if token, ok := s.Get(csrfSessionKey).(string); ok {
		token = strings.TrimSpace(token)
		if token != "" {
			c.Set(csrfContextKey, token)
			return token, nil
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	s.Set(csrfSessionKey, token)
	if err := s.Save(); err != nil {
		return "", err
	}
	c.Set(csrfContextKey, token)
	return token, nil
}

// CSRFToken 返回当前请求可用于模板渲染的 CSRF token。
func CSRFToken(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get(csrfContextKey); ok {
		if token, ok := v.(string); ok {
			return token
		}
	}
	if s := sessions.Default(c); s != nil {
		if token, ok := s.Get(csrfSessionKey).(string); ok {
			return strings.TrimSpace(token)
		}
	}
	return ""
}

// CSRFMiddleware 为后台会话型写操作提供同步 token + 同源校验。
func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := EnsureCSRFToken(c)
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		if isSafeMethod(c.Request.Method) {
			c.Next()
			return
		}
		if !sameOriginRequest(c.Request) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		submitted := strings.TrimSpace(c.GetHeader(csrfHeaderKey))
		if submitted == "" {
			submitted = strings.TrimSpace(c.PostForm(csrfFormKey))
		}
		if submitted == "" || subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) != 1 {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}

// VerifyCSRFToken 校验当前请求的 CSRF token 是否有效,返回 true 表示通过。
// 用于未经过 CSRFMiddleware 但需要 CSRF 保护的端点(如前台评论)。
func VerifyCSRFToken(c *gin.Context) bool {
	token, err := EnsureCSRFToken(c)
	if err != nil {
		return false
	}
	if !sameOriginRequest(c.Request) {
		return false
	}
	submitted := strings.TrimSpace(c.GetHeader(csrfHeaderKey))
	if submitted == "" {
		submitted = strings.TrimSpace(c.PostForm(csrfFormKey))
	}
	return submitted != "" && subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) == 1
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func sameOriginRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		origin = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(RequestScheme(r), u.Scheme) &&
		strings.EqualFold(RequestHost(r), u.Host)
}

func RequestScheme(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			v = v[:i]
		}
		return strings.ToLower(strings.TrimSpace(v))
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func RequestHost(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			v = v[:i]
		}
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(r.Host)
}
