package web_test

import (
	"bytes"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/youthlin/wenlog/internal/render"
	"github.com/youthlin/wenlog/web"
	webplugins "github.com/youthlin/wenlog/web/plugins"
)

func TestTemplatesParse(t *testing.T) {
	tplFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		t.Fatalf("sub templates fs: %v", err)
	}
	r, err := render.New(tplFS)
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	if r.Template() == nil {
		t.Fatal("template is nil")
	}
}

func TestEmbeddedTemplatesUseSnakeCaseFunctionNames(t *testing.T) {
	oldNames := []string{
		"postURL",
		"categoryURL",
		"tagURL",
		"safeHTML",
		"escapeHTML",
		"detailHTML",
		"hasMore",
		"avatarURL",
		"defaultAvatarURL",
		"fmtDate",
		"fmtDateTime",
		"fmtDateTimeLocal",
		"fmtFileSize",
		"toInt",
		"postExcerptHTML",
	}
	for _, root := range []struct {
		name string
		fsys fs.FS
	}{
		{name: "templates", fsys: web.Templates},
		{name: "themes", fsys: web.Themes},
		{name: "widgets", fsys: web.Widgets},
		{name: "plugins", fsys: webplugins.Plugins},
	} {
		err := fs.WalkDir(root.fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".gohtml") {
				return nil
			}
			data, err := fs.ReadFile(root.fsys, path)
			if err != nil {
				return err
			}
			for _, action := range templateActions(string(data)) {
				for _, oldName := range oldNames {
					if containsTemplateIdent(action, oldName) {
						t.Errorf("%s/%s uses legacy template function %q in action %q", root.name, path, oldName, action)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root.name, err)
		}
	}
}

func templateActions(s string) []string {
	var actions []string
	for {
		start := strings.Index(s, "{{")
		if start < 0 {
			return actions
		}
		s = s[start+2:]
		end := strings.Index(s, "}}")
		if end < 0 {
			return actions
		}
		actions = append(actions, s[:end])
		s = s[end+2:]
	}
}

func containsTemplateIdent(action, ident string) bool {
	for _, token := range strings.FieldsFunc(action, func(r rune) bool {
		return !(r == '_' || r == '$' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	}) {
		if strings.HasPrefix(token, "$") {
			continue
		}
		token = strings.TrimLeft(token, "$.")
		if token == ident {
			return true
		}
	}
	return false
}

func TestShortcodePatternHintWarnsAboutPrefixMatching(t *testing.T) {
	tplFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		t.Fatalf("sub templates fs: %v", err)
	}
	data, err := fs.ReadFile(tplFS, "admin_plugin_settings.gohtml")
	if err != nil {
		t.Fatalf("read plugin settings template: %v", err)
	}
	templateText := string(data)
	if !strings.Contains(templateText, `^https://host\.com(?:/|$)`) || !strings.Contains(templateText, `https://host.com.evil.com`) {
		t.Fatal("shortcode regex field must show an anchored host-boundary example and prefix-match warning")
	}
}

func TestMarkdownPreviewSubmitsFullPostForm(t *testing.T) {
	data, err := fs.ReadFile(web.Assets, "assets/admin.js")
	if err != nil {
		t.Fatalf("read admin script: %v", err)
	}
	script := string(data)
	if !strings.Contains(script, `new URLSearchParams(new FormData(postForm))`) {
		t.Fatal("Markdown preview must submit the full post form so hooks receive unsaved post fields")
	}
	if !strings.Contains(script, `customCSS: customCSS ? customCSS.value : ""`) {
		t.Fatal("local draft saving must include per-post custom CSS")
	}
}

func TestBuiltinThemesRenderPostCustomCSSOnSingularPages(t *testing.T) {
	for _, file := range []string{
		"themes/default/templates/header.gohtml",
		"themes/twentytwenty/templates/header.gohtml",
		"themes/spread/templates/header.gohtml",
		"themes/single/templates/index.gohtml",
	} {
		data, err := fs.ReadFile(web.Themes, file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		templateText := string(data)
		if !strings.Contains(templateText, `post_custom_css .Post`) {
			t.Fatalf("%s must render per-post custom CSS", file)
		}
		if !strings.Contains(templateText, `eq .PageType "post"`) || !strings.Contains(templateText, `eq .PageType "page"`) {
			t.Fatalf("%s must limit per-post custom CSS to post/page detail pages", file)
		}
	}
}

func TestAdminUIRegressionContracts(t *testing.T) {
	tplFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		t.Fatalf("sub templates fs: %v", err)
	}

	checks := []struct {
		path string
		want string
	}{
		{"admin_plugin_settings.gohtml", `<script src="/assets/admin.js?v={{.AssetVersion}}"></script>`},
		{"admin_menus.gohtml", `<button type="submit" class="btn">{{.t.T "保存菜单"}}</button>`},
		{"admin_import.gohtml", `class="cards settings-cards migration-cards"`},
		{"admin_post_edit.gohtml", `name="custom_css"`},
		{"admin_revision_view.gohtml", `post_custom_css .Revision`},
	}
	for _, check := range checks {
		data, err := fs.ReadFile(tplFS, check.path)
		if err != nil {
			t.Fatalf("read %s: %v", check.path, err)
		}
		if !strings.Contains(string(data), check.want) {
			t.Errorf("%s must contain %q", check.path, check.want)
		}
	}

	css, err := fs.ReadFile(web.Assets, "assets/admin.css")
	if err != nil {
		t.Fatalf("read admin stylesheet: %v", err)
	}
	for _, want := range []string{
		".theme-options-form textarea {\n  max-width: 100%;",
		".shortcode-rules {\n  width: 100%;",
		".shortcode-rules > .form-hint {\n  margin: 10px 0 0;",
		".migration-cards {\n  grid-template-columns: minmax(0, 1fr);",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("admin stylesheet must contain %q", want)
		}
	}

	themeYAML, err := fs.ReadFile(web.Themes, "themes/spread/theme.yaml")
	if err != nil {
		t.Fatalf("read spread theme manifest: %v", err)
	}
	if !strings.Contains(string(themeYAML), `default: "http://cn.bing.com/ImageResolution.aspx?w=1920&h=1200"`) {
		t.Error("spread hero image must use the configured Bing resolution endpoint")
	}
}

func TestAdminDashboardCommentCardLinksPendingWhenNeeded(t *testing.T) {
	tplFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		t.Fatalf("sub templates fs: %v", err)
	}
	data, err := fs.ReadFile(tplFS, "admin_dashboard.gohtml")
	if err != nil {
		t.Fatalf("read admin dashboard template: %v", err)
	}
	templateText := string(data)
	if !strings.Contains(templateText, `href="{{if gt .Stats.Comments.Pending 0}}/admin/comments?status=pending{{else}}/admin/comments{{end}}"`) {
		t.Fatalf("admin dashboard comment stats card must link to pending comments when pending count is positive")
	}
	if !strings.Contains(templateText, `.t.N1_64 "%d 条待审" .Stats.Comments.Pending .Stats.Comments.Pending`) {
		t.Fatalf("admin dashboard comment stats card should still display pending comment count")
	}
}

func TestAdminDataTablesFollowResponsiveContract(t *testing.T) {
	tplFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		t.Fatalf("sub templates fs: %v", err)
	}
	if err := fs.WalkDir(tplFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasPrefix(path, "admin_") || !strings.HasSuffix(path, ".gohtml") {
			return nil
		}
		data, err := fs.ReadFile(tplFS, path)
		if err != nil {
			return err
		}
		assertAdminDataTableContract(t, path, data)
		return nil
	}); err != nil {
		t.Fatalf("walk templates: %v", err)
	}
}

var (
	tableBlockRe = regexp.MustCompile(`(?is)<table\b[^>]*class="[^"]*\bdata-table\b[^"]*"[^>]*>.*?</table>`)
	classAttrRe  = regexp.MustCompile(`(?is)\bclass="([^"]*)"`)
	tdTagRe      = regexp.MustCompile(`(?is)<td\b[^>]*>`)
)

func assertAdminDataTableContract(t *testing.T, path string, data []byte) {
	t.Helper()
	matches := tableBlockRe.FindAllIndex(data, -1)
	for i, loc := range matches {
		block := data[loc[0]:loc[1]]
		openEnd := bytes.IndexByte(block, '>')
		if openEnd < 0 {
			t.Fatalf("%s table #%d opening tag is malformed", path, i+1)
		}
		openTag := string(block[:openEnd+1])
		classes := tableClasses(openTag)
		if !hasEntityTableClass(classes) {
			t.Fatalf("%s table #%d with data-table must also include an entity class ending with -table, classes=%q", path, i+1, strings.Join(classes, " "))
		}
		prefix := data[:loc[0]]
		lastScroll := bytes.LastIndex(prefix, []byte(`class="table-scroll`))
		lastTable := bytes.LastIndex(prefix, []byte("<table"))
		lastScrollClose := bytes.LastIndex(prefix, []byte("</div>"))
		if lastScroll < 0 || lastScroll < lastTable || lastScrollClose > lastScroll {
			t.Fatalf("%s table #%d must be wrapped by <div class=\"table-scroll...\">", path, i+1)
		}
		if !hasClass(classes, "import-stats-table") {
			for _, td := range tdTagRe.FindAll(block, -1) {
				tag := string(td)
				if strings.Contains(tag, "data-label=") || strings.Contains(tag, "colspan=") {
					continue
				}
				t.Fatalf("%s table #%d has <td> without data-label or colspan: %s", path, i+1, tag)
			}
		}
	}
}

func tableClasses(openTag string) []string {
	m := classAttrRe.FindStringSubmatch(openTag)
	if len(m) < 2 {
		return nil
	}
	return strings.Fields(m[1])
}

func hasEntityTableClass(classes []string) bool {
	for _, className := range classes {
		if className != "data-table" && strings.HasSuffix(className, "-table") {
			return true
		}
	}
	return false
}

func hasClass(classes []string, want string) bool {
	for _, className := range classes {
		if className == want {
			return true
		}
	}
	return false
}
