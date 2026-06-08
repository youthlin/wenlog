package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

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
