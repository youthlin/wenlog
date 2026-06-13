package render

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youthlin/blog/web"
)

func TestGravatar(t *testing.T) {
	// 已知:md5("me@example.com") = ... ;只校验前缀与不随大小写/空白变化。
	a := gravatar("Me@Example.com")
	b := gravatar("  me@example.com ")
	if a != b {
		t.Errorf("gravatar should normalize case/space: %q != %q", a, b)
	}
	if got := gravatar("me@example.com"); len(got) < len("https://cn.cravatar.com/avatar/")+32 {
		t.Errorf("gravatar url too short: %q", got)
	}
}

func TestHotRendererReload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sample.gohtml")
	if err := os.WriteFile(file, []byte(`{{define "sample"}}old{{end}}`), 0o644); err != nil {
		t.Fatalf("write old template: %v", err)
	}
	r, err := NewHot(dir)
	if err != nil {
		t.Fatalf("new hot renderer: %v", err)
	}
	if !r.Hot() {
		t.Fatal("renderer should be hot")
	}
	if got := execTemplate(t, r.Template(), "sample"); got != "old" {
		t.Fatalf("unexpected initial template output: %q", got)
	}
	if err := os.WriteFile(file, []byte(`{{define "sample"}}new{{end}}`), 0o644); err != nil {
		t.Fatalf("write new template: %v", err)
	}
	if got := execTemplate(t, r.Template(), "sample"); got != "old" {
		t.Fatalf("template should remain cached before reload: %q", got)
	}
	if err := r.Reload(); err != nil {
		t.Fatalf("reload templates: %v", err)
	}
	if got := execTemplate(t, r.Template(), "sample"); got != "new" {
		t.Fatalf("unexpected template output after reload: %q", got)
	}
}

func TestStaticRendererReleaseToHotDir(t *testing.T) {
	srcDir := t.TempDir()
	file := filepath.Join(srcDir, "sample.gohtml")
	if err := os.WriteFile(file, []byte(`{{define "sample"}}embed{{end}}`), 0o644); err != nil {
		t.Fatalf("write embedded-like template: %v", err)
	}
	r, err := New(os.DirFS(srcDir))
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	if r.Hot() {
		t.Fatal("renderer should start as static")
	}
	targetDir := filepath.Join(t.TempDir(), "templates")
	if err := r.ReleaseToHotDir(targetDir); err != nil {
		t.Fatalf("release templates: %v", err)
	}
	if !r.Hot() {
		t.Fatal("renderer should switch to hot after release")
	}
	if _, err := os.Stat(filepath.Join(targetDir, "sample.gohtml")); err != nil {
		t.Fatalf("released template file missing: %v", err)
	}
	if got := execTemplate(t, r.Template(), "sample"); got != "embed" {
		t.Fatalf("unexpected template output after release: %q", got)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "sample.gohtml"), []byte(`{{define "sample"}}hot{{end}}`), 0o644); err != nil {
		t.Fatalf("update released template: %v", err)
	}
	if err := r.Reload(); err != nil {
		t.Fatalf("reload released templates: %v", err)
	}
	if got := execTemplate(t, r.Template(), "sample"); got != "hot" {
		t.Fatalf("unexpected template output after release reload: %q", got)
	}
}

func TestPaginationTemplateKeepsPageContext(t *testing.T) {
	tplFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		t.Fatalf("sub templates fs: %v", err)
	}
	tpl, err := parseTemplates(tplFS, "*.gohtml")
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}

	data := map[string]any{
		"t": testTranslator{},
		"Pager": map[string]any{
			"Page":    2,
			"Pages":   3,
			"BaseURL": "/",
			"Sep":     "?",
		},
	}

	var b bytes.Buffer
	if err := tpl.ExecuteTemplate(&b, "pagination", data); err != nil {
		t.Fatalf("execute pagination: %v", err)
	}
	got := b.String()
	if !strings.Contains(got, `href="/"`) || !strings.Contains(got, "上一页") {
		t.Fatalf("pagination should render previous page link, got: %s", got)
	}
	if !strings.Contains(got, `href="/?page=3"`) || !strings.Contains(got, "下一页") {
		t.Fatalf("pagination should render next page link, got: %s", got)
	}
}

type testTranslator struct{}

func (testTranslator) T(message string, args ...any) string { return message }

func execTemplate(t *testing.T, tpl interface {
	ExecuteTemplate(wr io.Writer, name string, data any) error
}, name string) string {
	t.Helper()
	var b bytes.Buffer
	if err := tpl.ExecuteTemplate(&b, name, nil); err != nil {
		t.Fatalf("execute template %q: %v", name, err)
	}
	return b.String()
}
