package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return st
}

func TestEnsureInitialAdmin(t *testing.T) {
	st := newTestStore(t)
	if err := ensureInitialAdmin(st); err != nil {
		t.Fatalf("ensure initial admin: %v", err)
	}
	n, err := st.CountUsers()
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Fatalf("user count = %d, want 1", n)
	}
	u, err := st.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if u.PasswordHash == "" {
		t.Fatal("admin password hash is empty")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("")) == nil {
		t.Fatal("empty password unexpectedly matches")
	}
	if err := ensureInitialAdmin(st); err != nil {
		t.Fatalf("ensure initial admin second run: %v", err)
	}
	n, err = st.CountUsers()
	if err != nil {
		t.Fatalf("count users second run: %v", err)
	}
	if n != 1 {
		t.Fatalf("user count after second run = %d, want 1", n)
	}
}

func TestLocalFirstFileSystemSwitchesToLocalDir(t *testing.T) {
	localDir := filepath.Join(t.TempDir(), "assets")
	fallback := http.FS(fstest.MapFS{
		"style.css": &fstest.MapFile{Data: []byte("from-embed")},
	})
	fsys := localFirstFileSystem{dir: localDir, fallback: fallback}

	if got := readFileFromHTTPFS(t, fsys, "style.css"); got != "from-embed" {
		t.Fatalf("expected fallback content, got %q", got)
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatalf("mkdir local assets dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "style.css"), []byte("from-local"), 0o644); err != nil {
		t.Fatalf("write local asset: %v", err)
	}
	if got := readFileFromHTTPFS(t, fsys, "style.css"); got != "from-local" {
		t.Fatalf("expected local content, got %q", got)
	}
}

func TestEnsureInitialContent(t *testing.T) {
	st := newTestStore(t)
	if err := ensureInitialAdmin(st); err != nil {
		t.Fatalf("ensure initial admin: %v", err)
	}
	if err := ensureInitialContent(st); err != nil {
		t.Fatalf("ensure initial content: %v", err)
	}
	if err := ensureInitialContent(st); err != nil {
		t.Fatalf("ensure initial content second run: %v", err)
	}
	var posts []model.Post
	if err := st.DB().Order("id ASC").Find(&posts).Error; err != nil {
		t.Fatalf("list posts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("post count = %d, want 2", len(posts))
	}
	if posts[0].Title != "欢迎来到我的博客" || posts[0].PostType != model.PostTypePost {
		t.Fatalf("first post = %+v", posts[0])
	}
	if posts[1].Slug != "about" || posts[1].MenuOrder != 1 || posts[1].PostType != model.PostTypePage {
		t.Fatalf("about page = %+v", posts[1])
	}
	var comments []model.Comment
	if err := st.DB().Order("id ASC").Find(&comments).Error; err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comment count = %d, want 1", len(comments))
	}
	if comments[0].Status != model.CommentApproved || comments[0].Author != "youthlin" {
		t.Fatalf("comment = %+v", comments[0])
	}
}

func readFileFromHTTPFS(t *testing.T, fsys http.FileSystem, name string) string {
	t.Helper()
	f, err := fsys.Open(name)
	if err != nil {
		t.Fatalf("open %q: %v", name, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read %q: %v", name, err)
	}
	return string(data)
}
