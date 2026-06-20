package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func setupCSRFTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret-key-32bytes!!"))
	r.Use(sessions.Sessions("csrf_test", store))
	return r
}

func TestEnsureCSRFToken_GeneratesToken(t *testing.T) {
	r := setupCSRFTestRouter()
	r.GET("/test", func(c *gin.Context) {
		token, err := EnsureCSRFToken(c)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		if token == "" {
			c.String(http.StatusInternalServerError, "empty token")
			return
		}
		c.String(http.StatusOK, token)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 64 { // 32 bytes hex = 64 chars
		t.Fatalf("expected 64-char hex token, got %d chars", w.Body.Len())
	}
}

func TestEnsureCSRFToken_ReusesToken(t *testing.T) {
	r := setupCSRFTestRouter()
	r.GET("/test", func(c *gin.Context) {
		token, err := EnsureCSRFToken(c)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.String(http.StatusOK, token)
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w1, req1)
	token1 := w1.Body.String()

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)
	token2 := w2.Body.String()

	if token1 != token2 {
		t.Fatalf("expected same token across requests, got %q vs %q", token1, token2)
	}
}

func TestCSRFMiddleware_SafeMethodPasses(t *testing.T) {
	r := setupCSRFTestRouter()
	r.Use(CSRFMiddleware())
	r.GET("/safe", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/safe", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET, got %d", w.Code)
	}
}

func TestCSRFMiddleware_NoTokenBlocks(t *testing.T) {
	r := setupCSRFTestRouter()
	r.Use(CSRFMiddleware())
	r.POST("/unsafe", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unsafe", strings.NewReader("data=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for POST without token, got %d", w.Code)
	}
}

func TestCSRFMiddleware_ValidTokenPasses(t *testing.T) {
	r := setupCSRFTestRouter()
	r.Use(CSRFMiddleware())
	r.GET("/get-token", func(c *gin.Context) {
		tok, _ := EnsureCSRFToken(c)
		c.String(http.StatusOK, tok)
	})
	r.POST("/unsafe", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/get-token", nil)
	r.ServeHTTP(w1, req1)
	token := w1.Body.String()

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/unsafe", strings.NewReader("_csrf="+token))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("Origin", "http://127.0.0.1")
	req2.Host = "127.0.0.1"
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for POST with valid token, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestCSRFMiddleware_InvalidTokenBlocks(t *testing.T) {
	r := setupCSRFTestRouter()
	r.Use(CSRFMiddleware())
	r.GET("/get-token", func(c *gin.Context) {
		tok, _ := EnsureCSRFToken(c)
		c.String(http.StatusOK, tok)
	})
	r.POST("/unsafe", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/get-token", nil)
	r.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/unsafe", strings.NewReader("_csrf=wrongtoken"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("Origin", "http://127.0.0.1")
	req2.Host = "127.0.0.1"
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for POST with invalid token, got %d", w2.Code)
	}
}

func TestCSRFMiddleware_NoOriginBlocks(t *testing.T) {
	r := setupCSRFTestRouter()
	r.Use(CSRFMiddleware())
	r.GET("/get-token", func(c *gin.Context) {
		tok, _ := EnsureCSRFToken(c)
		c.String(http.StatusOK, tok)
	})
	r.POST("/unsafe", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/get-token", nil)
	r.ServeHTTP(w1, req1)
	token := w1.Body.String()

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/unsafe", strings.NewReader("_csrf="+token))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Host = "127.0.0.1"
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for POST without Origin, got %d", w2.Code)
	}
}

func TestVerifyCSRFToken_Valid(t *testing.T) {
	r := setupCSRFTestRouter()
	r.GET("/get-token", func(c *gin.Context) {
		tok, _ := EnsureCSRFToken(c)
		c.String(http.StatusOK, tok)
	})
	r.POST("/verify", func(c *gin.Context) {
		if VerifyCSRFToken(c) {
			c.String(http.StatusOK, "ok")
		} else {
			c.String(http.StatusForbidden, "bad")
		}
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/get-token", nil)
	r.ServeHTTP(w1, req1)
	token := w1.Body.String()

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/verify", strings.NewReader("_csrf="+token))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("Origin", "http://127.0.0.1")
	req2.Host = "127.0.0.1"
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid CSRF, got %d", w2.Code)
	}
}

func TestVerifyCSRFToken_Invalid(t *testing.T) {
	r := setupCSRFTestRouter()
	r.GET("/get-token", func(c *gin.Context) {
		tok, _ := EnsureCSRFToken(c)
		c.String(http.StatusOK, tok)
	})
	r.POST("/verify", func(c *gin.Context) {
		if VerifyCSRFToken(c) {
			c.String(http.StatusOK, "ok")
		} else {
			c.String(http.StatusForbidden, "bad")
		}
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/get-token", nil)
	r.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/verify", strings.NewReader("_csrf=wrong"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("Origin", "http://127.0.0.1")
	req2.Host = "127.0.0.1"
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for invalid CSRF, got %d", w2.Code)
	}
}

func TestRequestScheme(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		tls      bool
		expected string
	}{
		{"default http", "", false, "http"},
		{"tls", "", true, "https"},
		{"x-forwarded-proto https", "https", false, "https"},
		{"x-forwarded-proto with comma", "https, http", false, "https"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("X-Forwarded-Proto", tt.header)
			}
			if tt.tls {
				req.TLS = &tls.ConnectionState{}
			}
			got := RequestScheme(req)
			if got != tt.expected {
				t.Errorf("RequestScheme() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRequestHost(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		host     string
		expected string
	}{
		{"host only", "", "example.com", "example.com"},
		{"x-forwarded-host", "proxy.example.com", "backend.local", "proxy.example.com"},
		{"x-forwarded-host with comma", "a.example.com, b.example.com", "backend", "a.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host
			if tt.header != "" {
				req.Header.Set("X-Forwarded-Host", tt.header)
			}
			got := RequestHost(req)
			if got != tt.expected {
				t.Errorf("RequestHost() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIsSafeMethod(t *testing.T) {
	if !isSafeMethod(http.MethodGet) {
		t.Error("GET should be safe")
	}
	if !isSafeMethod(http.MethodHead) {
		t.Error("HEAD should be safe")
	}
	if !isSafeMethod(http.MethodOptions) {
		t.Error("OPTIONS should be safe")
	}
	if isSafeMethod(http.MethodPost) {
		t.Error("POST should not be safe")
	}
	if isSafeMethod(http.MethodPut) {
		t.Error("PUT should not be safe")
	}
	if isSafeMethod(http.MethodDelete) {
		t.Error("DELETE should not be safe")
	}
}

func TestCSRFToken_NilContext(t *testing.T) {
	if token := CSRFToken(nil); token != "" {
		t.Errorf("CSRFToken(nil) = %q, want empty", token)
	}
}

func TestEnsureCSRFToken_NilContext(t *testing.T) {
	token, err := EnsureCSRFToken(nil)
	if err != nil {
		t.Errorf("EnsureCSRFToken(nil) error = %v", err)
	}
	if token != "" {
		t.Errorf("EnsureCSRFToken(nil) = %q, want empty", token)
	}
}
