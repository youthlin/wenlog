package handler

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
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
