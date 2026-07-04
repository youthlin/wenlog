package render

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/web"
)

func TestAvatarURL(t *testing.T) {
	// 已知:md5("me@example.com") = ... ;只校验前缀与不随大小写/空白变化。
	a := avatarURL("Me@Example.com", "")
	b := avatarURL("  me@example.com ", "")
	if a != b {
		t.Errorf("avatarURL should normalize case/space: %q != %q", a, b)
	}
	if got := avatarURL("me@example.com", ""); len(got) < len("https://cn.cravatar.com/avatar/")+32 {
		t.Errorf("avatar url too short: %q", got)
	}
}

func TestPostNavigationSkipsTypedNilPosts(t *testing.T) {
	data := map[string]any{
		"PrevPost": (*model.Post)(nil),
		"NextPost": (*model.Post)(nil),
	}
	if got := postNavigation(nil, data); got != "" {
		t.Fatalf("postNavigation with typed nil posts = %q, want empty", got)
	}
	if got := postURL((*model.Post)(nil)); got != "" {
		t.Fatalf("postURL with typed nil post = %q, want empty", got)
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
	tplFS, err := fs.Sub(web.Themes, "themes/default/templates")
	if err != nil {
		t.Fatalf("sub default theme templates fs: %v", err)
	}
	tpl, err := parseTemplates(tplFS, "*.gohtml")
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}

	data := map[string]any{
		"t":  testTranslator{},
		"th": testTranslator{},
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

func TestHighlightCodeBlocksSupportsLegacyPre(t *testing.T) {
	got := HighlightCodeBlocks(`<p>legacy</p><pre>package main

func main() {}
</pre>`)
	if !strings.Contains(got, `class="codehilite"`) || !strings.Contains(got, `<table`) {
		t.Fatalf("legacy pre block should be highlighted, got: %s", got)
	}
	if strings.Contains(got, `<pre>package main`) {
		t.Fatalf("legacy pre block was not replaced: %s", got)
	}
}

func TestThemeRenderStateIsRequestScoped(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFile(t, dir, "page.gohtml", `{{define "page"}}{{hook_invoke "loader"}}/{{hook_invoke "theme"}}|{{render_widgets "sidebar" .}}{{end}}`)
	writeTemplateFile(t, dir, "widget_alpha.gohtml", `{{define "widget_alpha"}}{{widget_option "name"}}{{end}}`)
	r, err := NewHot(dir)
	if err != nil {
		t.Fatalf("new hot renderer: %v", err)
	}

	r.SetHookInvokeProvider(func(ctx *RequestContext, name string, args ...any) any {
		switch name {
		case "loader":
			return ctx.ThemeLoader
		case "theme":
			return ctx.Theme
		default:
			return nil
		}
	})
	r.SetThemeWidgetsProvider(func(ctx *RequestContext, area string) []WidgetInfo {
		loader, _ := ctx.ThemeLoader.(string)
		theme, _ := ctx.Theme.(string)
		return []WidgetInfo{{
			Source:       "theme",
			ID:           "alpha",
			TemplateName: "widget_alpha",
			Options:      map[string]string{"name": loader + "/" + theme},
		}}
	})
	r.SetWidgetResolver(func(source, id, pluginID string) hook.Widget {
		if source == "theme" && id == "alpha" {
			return &testTemplateWidget{id: id}
		}
		return nil
	})
	t.Cleanup(func() {
		r.SetHookInvokeProvider(nil)
		r.SetThemeWidgetsProvider(nil)
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		want := string(rune('A' + i%26))
		wg.Add(1)
		go func() {
			defer wg.Done()
			rr := httptest.NewRecorder()
			data := map[string]any{ThemeLoaderDataKey: want, ThemeDataKey: want}
			if err := r.Instance("page", data).Render(rr); err != nil {
				t.Errorf("render template: %v", err)
				return
			}
			wantOutput := want + "/" + want + "|" + want + "/" + want
			if got := rr.Body.String(); got != wantOutput {
				t.Errorf("render state leaked: got %q, want %q", got, wantOutput)
			}
		}()
	}
	wg.Wait()
}

func TestBuiltinWidgetFilesDoNotShadowPageTemplates(t *testing.T) {
	tplFS, err := fs.Sub(web.Themes, "themes/default/templates")
	if err != nil {
		t.Fatalf("sub default theme templates fs: %v", err)
	}
	r, err := New(tplFS)
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	if err := r.loadThemeTemplatesFS(tplFS); err != nil {
		t.Fatalf("load theme templates: %v", err)
	}
	if got := r.ResolveTemplate("search"); got != "list.gohtml" {
		t.Fatalf("ResolveTemplate(search)=%q, want list.gohtml fallback", got)
	}
	if !r.HasTemplate("widget_search") {
		t.Fatal("builtin search widget should still be available")
	}
}

func TestResolveTemplateUsesSpecificFallbacks(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFile(t, dir, "index.gohtml", `index`)
	writeTemplateFile(t, dir, "page.gohtml", `page`)
	writeTemplateFile(t, dir, "page-about.gohtml", `about`)
	writeTemplateFile(t, dir, "post-123.gohtml", `post123`)
	writeTemplateFile(t, dir, "category-go.gohtml", `catgo`)
	r, err := NewHot(dir)
	if err != nil {
		t.Fatalf("new hot renderer: %v", err)
	}

	pageData := map[string]any{"Post": struct {
		ID   uint
		Slug string
	}{ID: 7, Slug: "about"}}
	if got := r.ResolveTemplateWithPreviewData("page", "", pageData); got != "page-about.gohtml" {
		t.Fatalf("ResolveTemplateWithPreviewData(page)=%q, want page-about.gohtml", got)
	}
	postData := map[string]any{"Post": struct {
		ID   uint
		Slug string
	}{ID: 123}}
	if got := r.ResolveTemplateWithPreviewData("post", "", postData); got != "post-123.gohtml" {
		t.Fatalf("ResolveTemplateWithPreviewData(post)=%q, want post-123.gohtml", got)
	}
	catData := map[string]any{"CategorySlug": "go"}
	if got := r.ResolveTemplateWithPreviewData("category", "", catData); got != "category-go.gohtml" {
		t.Fatalf("ResolveTemplateWithPreviewData(category)=%q, want category-go.gohtml", got)
	}
}

func TestRenderMenuRendersPrimaryLocation(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFile(t, dir, "index.gohtml", `{{define "index"}}{{render_menu "primary" .}}{{end}}`)
	r, err := NewHot(dir)
	if err != nil {
		t.Fatalf("new hot renderer: %v", err)
	}
	data := map[string]any{"Menus": map[string]any{"primary": []any{map[string]any{"Title": "About", "URL": "/about"}}}}
	rr := httptest.NewRecorder()
	if err := r.Instance("index", data).Render(rr); err != nil {
		t.Fatalf("render menu: %v", err)
	}
	got := rr.Body.String()
	if !strings.Contains(got, `class="menu menu-primary"`) || !strings.Contains(got, `href="/about"`) || !strings.Contains(got, `About`) {
		t.Fatalf("rendered menu missing expected output: %s", got)
	}
}

func TestRenderMenuRendersChildrenAndCustomURL(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFile(t, dir, "index.gohtml", `{{define "index"}}{{render_menu "primary" .}}{{end}}`)
	r, err := NewHot(dir)
	if err != nil {
		t.Fatalf("new hot renderer: %v", err)
	}
	data := map[string]any{"Menus": map[string]any{"primary": []any{map[string]any{"Title": "Docs", "URL": "/docs", "Children": []any{map[string]any{"Title": "GitHub", "URL": "https://github.com", "Target": "_blank"}}}}}}
	rr := httptest.NewRecorder()
	if err := r.Instance("index", data).Render(rr); err != nil {
		t.Fatalf("render menu: %v", err)
	}
	got := rr.Body.String()
	if !strings.Contains(got, `class="sub-menu"`) || !strings.Contains(got, `href="https://github.com" target="_blank"`) {
		t.Fatalf("rendered submenu missing expected output: %s", got)
	}
}

type testTranslator struct{}

func (testTranslator) T(message string, args ...any) string { return message }

// testTemplateWidget 是测试用的模板型组件，实现 hook.Widget 接口。
type testTemplateWidget struct {
	id string
}

func (w *testTemplateWidget) Meta() hook.WidgetDecl {
	return hook.WidgetDecl{ID: w.id, Source: "theme"}
}

func (w *testTemplateWidget) Render(ctx context.Context, tpl *template.Template, instance hook.WidgetInstance, data any) (template.HTML, error) {
	var buf strings.Builder
	name := "widget_" + w.id
	if err := tpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

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

func writeTemplateFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write template %s: %v", name, err)
	}
}
