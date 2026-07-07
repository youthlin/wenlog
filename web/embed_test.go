package web_test

import (
	"bytes"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/youthlin/wenlog/internal/render"
	"github.com/youthlin/wenlog/web"
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
