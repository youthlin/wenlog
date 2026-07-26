package extension

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeRelativePath(t *testing.T) {
	for _, path := range []string{"theme/style.css", "plugin.yaml", "dir/../file.txt"} {
		if !SafeRelativePath(filepath.Clean(path)) {
			t.Fatalf("SafeRelativePath(%q)=false, want true", path)
		}
	}
	for _, path := range []string{".", "..", "../x", filepath.Join("..", "x"), filepath.Clean("/tmp/x"), "bad\x00path"} {
		if SafeRelativePath(path) {
			t.Fatalf("SafeRelativePath(%q)=true, want false", path)
		}
	}
}

func TestPathWithinDir(t *testing.T) {
	dir := t.TempDir()
	if !PathWithinDir(dir, filepath.Join(dir, "a", "b.txt")) {
		t.Fatalf("PathWithinDir(child)=false, want true")
	}
	if PathWithinDir(dir, filepath.Join(dir, "..", "escape.txt")) {
		t.Fatalf("PathWithinDir(escape)=true, want false")
	}
}

func TestExtractZipSkipsUnsafeAndDisallowedFiles(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	addZipFile(t, zw, "demo/allowed.txt", "ok")
	addZipFile(t, zw, "../escape.txt", "bad")
	addZipFile(t, zw, "demo/skip.exe", "bad")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	err := ExtractZip(bytes.NewReader(buf.Bytes()), int64(buf.Len()), dest, ExtractOptions{
		Kind:       "test",
		MaxSize:    1024,
		MaxFile:    512,
		MaxFiles:   10,
		MaxNameLen: 255,
		AllowFile: func(name string) bool {
			return filepath.Ext(name) == ".txt"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "demo", "allowed.txt")); err != nil {
		t.Fatalf("allowed file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("unsafe file exists or unexpected stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "demo", "skip.exe")); !os.IsNotExist(err) {
		t.Fatalf("disallowed file exists or unexpected stat error: %v", err)
	}
}

func TestBackupDirAndRollbackReplace(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "demo")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "file.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	backup, err := BackupDir(target, "test")
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("BackupDir returned empty backup")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target still exists or unexpected stat error: %v", err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RollbackReplace(target, backup); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("restored file = %q, want old", data)
	}
}

func addZipFile(t *testing.T, zw *zip.Writer, name, content string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}
