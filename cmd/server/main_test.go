package main

import (
	"testing"

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
