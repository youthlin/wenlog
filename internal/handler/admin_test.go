package handler

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/youthlin/blog/internal/model"
)

func TestAllowDebugSQL(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{name: "select", sql: "SELECT * FROM posts", want: true},
		{name: "explain", sql: "EXPLAIN QUERY PLAN SELECT * FROM posts", want: true},
		{name: "pragma denied", sql: "PRAGMA journal_mode=WAL", want: false},
		{name: "show denied", sql: "SHOW TABLES", want: false},
	}
	for _, tt := range tests {
		if got := allowDebugSQL(tt.sql); got != tt.want {
			t.Fatalf("%s: allowDebugSQL(%q) = %v, want %v", tt.name, tt.sql, got, tt.want)
		}
	}
}

func TestValidatePageSlug(t *testing.T) {
	tests := []struct {
		name string
		slug string
		ok   bool
	}{
		{name: "normal", slug: "about", ok: true},
		{name: "reserved", slug: "search", ok: false},
		{name: "post permalink style", slug: "2024123.html", ok: false},
		{name: "multi segment", slug: "foo/bar", ok: false},
		{name: "blank", slug: " ", ok: false},
	}
	for _, tt := range tests {
		err := validatePageSlug(tt.slug)
		if (err == nil) != tt.ok {
			t.Fatalf("%s: validatePageSlug(%q) err=%v, want ok=%v", tt.name, tt.slug, err, tt.ok)
		}
	}
}

func TestReleaseDirFromFS(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "assets")
	src := fstest.MapFS{
		"style.css":                &fstest.MapFile{Data: []byte("body{}")},
		"nested/app.js":            &fstest.MapFile{Data: []byte("console.log(1)")},
		"nested/deeper/theme.json": &fstest.MapFile{Data: []byte(`{"dark":true}`)},
	}
	if err := releaseDirFromFS(src, targetDir); err != nil {
		t.Fatalf("release dir from fs: %v", err)
	}
	for path, file := range src {
		got, err := os.ReadFile(filepath.Join(targetDir, path))
		if err != nil {
			t.Fatalf("read released file %q: %v", path, err)
		}
		if string(got) != string(file.Data) {
			t.Fatalf("released file %q mismatch: got %q want %q", path, got, file.Data)
		}
	}
	if !pathExists(targetDir) {
		t.Fatal("target dir should exist after release")
	}
	if pathExists(filepath.Join(targetDir, "missing.txt")) {
		t.Fatal("missing file should not exist")
	}
	if _, err := fs.Stat(os.DirFS(targetDir), "nested/deeper/theme.json"); err != nil {
		t.Fatalf("stat nested released file: %v", err)
	}
}

func TestNormalizeTermSlug(t *testing.T) {
	tests := map[string]string{
		" Go 教程 ":       "go-教程",
		"Hello, World!": "hello-world",
		"中文 标签":         "中文-标签",
	}
	for in, want := range tests {
		if got := normalizeTermSlug(in); got != want {
			t.Fatalf("normalizeTermSlug(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestSpecialPageFallsBackWithoutStoredPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Public{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/archive", nil)
	p := h.specialPage(c, "archive", "归档")
	if p == nil {
		t.Fatal("specialPage returned nil")
	}
	if p.Slug != "archive" || p.Title != "归档" {
		t.Fatalf("specialPage = %+v", p)
	}
	if p.PostType != "" && p.PostType != model.PostTypePage {
		t.Fatalf("unexpected post type: %+v", p)
	}
}

func TestCommentStatusForUser(t *testing.T) {
	target := &model.Post{AuthorID: 2}
	tests := []struct {
		name string
		user *model.User
		want string
	}{
		{name: "anonymous", user: nil, want: model.CommentPending},
		{name: "subscriber", user: &model.User{ID: 3, Role: model.RoleSubscriber}, want: model.CommentPending},
		{name: "admin", user: &model.User{ID: 3, Role: model.RoleAdmin}, want: model.CommentApproved},
		{name: "post author", user: &model.User{ID: 2, Role: model.RoleAuthor}, want: model.CommentApproved},
	}
	for _, tt := range tests {
		if got := commentStatusForUser(tt.user, target); got != tt.want {
			t.Fatalf("%s: commentStatusForUser()=%q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestCanEditOwnUsername(t *testing.T) {
	tests := []struct {
		name string
		user *model.User
		want bool
	}{
		{name: "nil", user: nil, want: false},
		{name: "admin", user: &model.User{Role: model.RoleAdmin}, want: true},
		{name: "author", user: &model.User{Role: model.RoleAuthor}, want: false},
		{name: "subscriber", user: &model.User{Role: model.RoleSubscriber}, want: false},
	}
	for _, tt := range tests {
		if got := canEditOwnUsername(tt.user); got != tt.want {
			t.Fatalf("%s: canEditOwnUsername()=%v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRememberCommenter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("test_session", cookie.NewStore([]byte("secret"))))
	r.GET("/set", func(c *gin.Context) {
		rememberCommenter(c, commentReq{
			Author: " Alice ",
			Email:  " alice@example.com ",
			URL:    " https://example.com ",
		})
		c.Status(http.StatusNoContent)
	})
	r.GET("/get", func(c *gin.Context) {
		got := rememberedCommenter(c)
		if got["Author"] != "Alice" || got["Email"] != "alice@example.com" || got["URL"] != "https://example.com" {
			t.Fatalf("rememberedCommenter()=%v", got)
		}
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/set", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("set status=%d", w.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/get", nil)
	for _, cookie := range w.Result().Cookies() {
		req.AddCookie(cookie)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("get status=%d", w.Code)
	}
}
