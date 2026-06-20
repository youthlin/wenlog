package handler

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/web"
)

func setupAuthHandlerTest(t *testing.T) (*gin.Engine, *Auth, *store.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dbPath := t.TempDir() + "/test.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	auth := NewAuth(st, log)

	r := gin.New()
	store := cookie.NewStore([]byte("test-auth-handler-secret-32b"))
	r.Use(sessions.Sessions("auth_handler_test", store))

	// Load templates from embed FS so c.HTML() works
	tplFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		t.Fatalf("sub templates: %v", err)
	}
	renderer, err := render.New(tplFS)
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	r.HTMLRender = renderer

	return r, auth, st
}

func createTestUser(t *testing.T, st *store.Store, username, password, role string) *model.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u := &model.User{
		Username:     username,
		PasswordHash: string(hash),
		DisplayName:  username,
		Role:         role,
	}
	if err := st.DB().Create(u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func TestLoginForm_ShowsLoginPage(t *testing.T) {
	r, auth, _ := setupAuthHandlerTest(t)
	r.GET("/auth/login", auth.LoginForm)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "登录") {
		t.Error("login page should contain 登录")
	}
}

func TestLogin_Success(t *testing.T) {
	r, auth, st := setupAuthHandlerTest(t)
	createTestUser(t, st, "admin", "password123", model.RoleAdmin)

	r.POST("/auth/login", auth.Login)

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "password123")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d: %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if loc != "/admin/" {
		t.Fatalf("expected redirect to /admin/, got %q", loc)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	r, auth, st := setupAuthHandlerTest(t)
	createTestUser(t, st, "admin", "password123", model.RoleAdmin)

	r.POST("/auth/login", auth.Login)

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "wrongpassword")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "用户名或密码错误") {
		t.Error("should show error message")
	}
}

func TestLogin_NonexistentUser(t *testing.T) {
	r, auth, _ := setupAuthHandlerTest(t)

	r.POST("/auth/login", auth.Login)

	form := url.Values{}
	form.Set("username", "nobody")
	form.Set("password", "whatever")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	r, auth, st := setupAuthHandlerTest(t)
	createTestUser(t, st, "admin", "password123", model.RoleAdmin)

	r.POST("/auth/login", auth.Login)
	r.GET("/auth/logout", auth.Logout)

	// First login
	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "password123")
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w1, req1)

	// Then logout with session cookie
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", w2.Code)
	}
	loc := w2.Header().Get("Location")
	if loc != "/auth/login" {
		t.Fatalf("expected redirect to /auth/login, got %q", loc)
	}
}

func TestLogout_ClearsSession(t *testing.T) {
	r, auth, st := setupAuthHandlerTest(t)
	createTestUser(t, st, "admin", "password123", model.RoleAdmin)

	r.POST("/auth/login", auth.Login)
	r.GET("/auth/logout", auth.Logout)
	r.GET("/admin", middleware.AuthRequired(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Login
	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "password123")
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w1, req1)

	// Logout
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	for _, c := range w1.Result().Cookies() {
		req2.AddCookie(c)
	}
	r.ServeHTTP(w2, req2)

	// Try accessing admin with logout cookies (should redirect)
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	for _, c := range w2.Result().Cookies() {
		req3.AddCookie(c)
	}
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect after logout, got %d", w3.Code)
	}
}

func TestLoginForm_ShowsSessionSecretNotice(t *testing.T) {
	r, auth, _ := setupAuthHandlerTest(t)
	r.GET("/auth/login", auth.LoginForm)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/login?message=session-secret-updated", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Session Secret") {
		t.Error("should show session secret notice")
	}
}

func TestForgotPasswordForm_ShowsPage(t *testing.T) {
	r, auth, _ := setupAuthHandlerTest(t)
	r.GET("/auth/forgot-password", auth.ForgotPasswordForm)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/forgot-password", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "忘记密码") {
		t.Error("should show forgot password page")
	}
}

func TestForgotPassword_EmptyEmail(t *testing.T) {
	r, auth, _ := setupAuthHandlerTest(t)
	r.POST("/auth/forgot-password", auth.ForgotPassword)

	form := url.Values{}
	form.Set("email", "")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "请输入邮箱地址") {
		t.Error("should show email required message")
	}
}

func TestForgotPassword_NonexistentEmail(t *testing.T) {
	r, auth, _ := setupAuthHandlerTest(t)
	r.POST("/auth/forgot-password", auth.ForgotPassword)

	form := url.Values{}
	form.Set("email", "nobody@example.com")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	// Should still return 200 with success message (don't reveal if email exists)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "如果该邮箱已注册") {
		t.Error("should show generic success message")
	}
}

func TestResetPasswordForm_NoToken(t *testing.T) {
	r, auth, _ := setupAuthHandlerTest(t)
	r.GET("/auth/reset-password", auth.ResetPasswordForm)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/reset-password", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
}

func TestResetPasswordForm_InvalidToken(t *testing.T) {
	r, auth, _ := setupAuthHandlerTest(t)
	r.GET("/auth/reset-password", auth.ResetPasswordForm)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/reset-password?token=invalid", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "重置链接无效") {
		t.Error("should show invalid token message")
	}
}

func TestRegisterForm_ClosedRegistration(t *testing.T) {
	r, auth, _ := setupAuthHandlerTest(t)
	r.GET("/auth/register", auth.RegisterForm)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/register", nil)
	r.ServeHTTP(w, req)

	// Registration is closed by default
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
}

func TestRegister_ClosedRegistration(t *testing.T) {
	r, auth, _ := setupAuthHandlerTest(t)
	r.POST("/auth/register", auth.Register)

	form := url.Values{}
	form.Set("username", "newuser")
	form.Set("email", "new@example.com")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
}
