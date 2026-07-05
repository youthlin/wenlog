package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func setupAuthTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-auth-secret-key-32bytes"))
	r.Use(sessions.Sessions("auth_test", store))
	return r
}

func TestAuthRequired_NoSessionRedirects(t *testing.T) {
	r := setupAuthTestRouter()
	r.Use(AuthRequired())
	r.GET("/admin", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/auth/login" {
		t.Fatalf("expected redirect to /auth/login, got %q", loc)
	}
}

func TestAuthRequired_WithSessionPasses(t *testing.T) {
	r := setupAuthTestRouter()
	// Apply AuthRequired only to /admin, not globally — otherwise /set-session would also be blocked.
	r.GET("/admin", AuthRequired(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// First set session via a helper route (no auth required)
	r.GET("/set-session", func(c *gin.Context) {
		SetSessionUser(c, 1, "admin")
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/set-session", nil)
	r.ServeHTTP(w1, req1)

	// Now access /admin with the session cookie
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 with session, got %d", w2.Code)
	}
}

func TestAuthRequiredRedirect_CustomPath(t *testing.T) {
	r := setupAuthTestRouter()
	r.Use(AuthRequiredRedirect("/custom-login"))
	r.GET("/admin", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/custom-login" {
		t.Fatalf("expected redirect to /custom-login, got %q", loc)
	}
}

func TestRequireRole_AdminPasses(t *testing.T) {
	r := setupAuthTestRouter()
	// Apply RequireRole only to the protected route, not globally.
	r.GET("/admin-only", RequireRole("admin"), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Set admin role (no role check needed)
	r.GET("/set-admin", func(c *gin.Context) {
		SetSessionUser(c, 1, "admin")
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/set-admin", nil)
	r.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin role, got %d", w2.Code)
	}
}

func TestRequireRole_SubscriberBlocked(t *testing.T) {
	r := setupAuthTestRouter()
	r.GET("/admin-only", RequireRole("admin"), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	r.GET("/set-subscriber", func(c *gin.Context) {
		SetSessionUser(c, 2, "subscriber")
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/set-subscriber", nil)
	r.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for subscriber role, got %d", w2.Code)
	}
}

func TestRequireRole_MultipleRoles(t *testing.T) {
	r := setupAuthTestRouter()
	r.GET("/content", RequireRole("admin", "author"), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	r.GET("/set-author", func(c *gin.Context) {
		SetSessionUser(c, 3, "author")
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/set-author", nil)
	r.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/content", nil)
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for author role (in list), got %d", w2.Code)
	}
}

func TestSetSessionUser(t *testing.T) {
	r := setupAuthTestRouter()
	r.GET("/login", func(c *gin.Context) {
		SetSessionUser(c, 42, "admin", 5)
		c.String(http.StatusOK, "ok")
	})
	r.GET("/check", func(c *gin.Context) {
		s := sessions.Default(c)
		uid := s.Get(SessionUserKey)
		role := s.Get(SessionRoleKey)
		ver := s.Get(SessionVersionKey)
		c.JSON(http.StatusOK, gin.H{"uid": uid, "role": role, "ver": ver})
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/login", nil)
	r.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/check", nil)
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
	body := w2.Body.String()
	if !contains(body, `"uid":42`) {
		t.Errorf("expected uid=42 in response, got %s", body)
	}
	if !contains(body, `"role":"admin"`) {
		t.Errorf("expected role=admin in response, got %s", body)
	}
}

func TestClearSession(t *testing.T) {
	r := setupAuthTestRouter()
	r.GET("/login", func(c *gin.Context) {
		SetSessionUser(c, 1, "admin")
		c.String(http.StatusOK, "ok")
	})
	r.GET("/logout", func(c *gin.Context) {
		ClearSession(c)
		c.String(http.StatusOK, "ok")
	})
	r.GET("/check", func(c *gin.Context) {
		s := sessions.Default(c)
		uid := s.Get(SessionUserKey)
		if uid == nil {
			c.String(http.StatusOK, "no-session")
		} else {
			c.String(http.StatusOK, "has-session")
		}
	})

	// Login
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/login", nil)
	r.ServeHTTP(w1, req1)

	// Logout
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/logout", nil)
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	// Check session cleared
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/check", nil)
	for _, c := range w2.Result().Cookies() {
		req3.AddCookie(c)
	}
	r.ServeHTTP(w3, req3)

	if w3.Body.String() != "no-session" {
		t.Fatalf("expected no-session after logout, got %s", w3.Body.String())
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
