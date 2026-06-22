package theme

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testSettingStore struct{}

func (testSettingStore) GetSetting(context.Context, string) (string, error) { return "", nil }
func (testSettingStore) SetSetting(context.Context, string, string) error   { return nil }

func TestThemeFilePathRejectsTraversalAndPrefixSibling(t *testing.T) {
	root := t.TempDir()
	themeDir := filepath.Join(root, "default")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatalf("create theme dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "theme.yaml"), []byte("name: default\n"), 0o644); err != nil {
		t.Fatalf("write theme yaml: %v", err)
	}
	m, err := NewManager(root, testSettingStore{})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	got, err := m.ThemeFilePath("default", "templates/index.gohtml")
	if err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	wantPrefix, _ := filepath.Abs(themeDir)
	if rel, err := filepath.Rel(wantPrefix, got); err != nil || rel == ".." || filepath.IsAbs(rel) {
		t.Fatalf("valid path should stay in theme dir, got %q rel %q err %v", got, rel, err)
	}

	badPaths := []string{
		"../default2/theme.yaml",
		"../../etc/passwd",
		filepath.Join("..", "default2", "theme.yaml"),
		filepath.Join(root, "default", "theme.yaml"),
	}
	for _, p := range badPaths {
		if _, err := m.ThemeFilePath("default", p); err == nil {
			t.Fatalf("path %q should be rejected", p)
		}
	}
}

func TestManagerInstallReplacesThemeAtomically(t *testing.T) {
	root := t.TempDir()
	if err := writeTestTheme(filepath.Join(root, "themes", "custom"), "custom", "old"); err != nil {
		t.Fatalf("write old theme: %v", err)
	}
	m, err := NewManager(filepath.Join(root, "themes"), testSettingStore{})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	src := filepath.Join(root, "upload", "custom")
	if err := writeTestTheme(src, "custom", "new"); err != nil {
		t.Fatalf("write uploaded theme: %v", err)
	}
	installed, err := m.Install(src)
	if err != nil {
		t.Fatalf("install theme: %v", err)
	}
	if installed == nil || installed.Name != "custom" {
		t.Fatalf("unexpected installed theme: %#v", installed)
	}
	data, err := os.ReadFile(filepath.Join(root, "themes", "custom", "templates", "index.gohtml"))
	if err != nil {
		t.Fatalf("read installed template: %v", err)
	}
	if string(data) != "new" {
		t.Fatalf("installed template = %q, want new", data)
	}
	entries, err := os.ReadDir(filepath.Join(root, "themes"))
	if err != nil {
		t.Fatalf("read themes dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".install-") || strings.HasPrefix(entry.Name(), ".backup-") {
			t.Fatalf("temporary install directory was not cleaned up: %s", entry.Name())
		}
	}
}

func writeTestTheme(dir, name, indexContent string) error {
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: "+name+"\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "templates", "index.gohtml"), []byte(indexContent), 0o644)
}
