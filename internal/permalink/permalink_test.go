package permalink

import (
	"testing"
	"time"

	"github.com/youthlin/wenlog/internal/model"
)

func TestPost(t *testing.T) {
	SetPostPattern("/%year%%post_id%.html")
	p := &model.Post{ID: 8, PublishedAt: time.Date(2012, 12, 6, 0, 0, 0, 0, time.UTC)}
	if got := Post(p); got != "/20128.html" {
		t.Errorf("Post() = %q, want /20128.html", got)
	}
	p2 := &model.Post{ID: 1046, PublishedAt: time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)}
	if got := Post(p2); got != "/20151046.html" {
		t.Errorf("Post() = %q, want /20151046.html", got)
	}
}

func TestParsePostPath(t *testing.T) {
	SetPostPattern("/%year%%post_id%.html")
	tests := []struct {
		path     string
		wantYear int
		wantID   uint
		wantOK   bool
	}{
		{"/20128.html", 2012, 8, true},
		{"/20151046.html", 2015, 1046, true},
		{"/about", 0, 0, false},
		{"/", 0, 0, false},
		{"/2012.html", 0, 0, false}, // 没有 id 部分则无法区分,需至少 4+1 位
	}
	for _, tt := range tests {
		match, ok := ParsePostPath(tt.path)
		year, id := 0, uint(0)
		if ok {
			year, id = match.Year, match.PostID
		}
		if ok != tt.wantOK || (ok && (year != tt.wantYear || id != tt.wantID)) {
			t.Errorf("ParsePostPath(%q) = (%d,%d,%v), want (%d,%d,%v)",
				tt.path, year, id, ok, tt.wantYear, tt.wantID, tt.wantOK)
		}
	}
}

func TestCustomPattern(t *testing.T) {
	SetPostPattern("/%year%/%monthnum%/%postname%/")
	p := &model.Post{
		ID:          42,
		Slug:        "hello-go",
		PublishedAt: time.Date(2026, 6, 11, 9, 8, 7, 0, time.UTC),
	}
	if got := Post(p); got != "/2026/06/hello-go/" {
		t.Fatalf("Post() = %q", got)
	}
	match, ok := ParsePostPath("/2026/06/hello-go/")
	if !ok {
		t.Fatal("ParsePostPath should match custom pattern")
	}
	if !match.HasYear || match.Year != 2026 || !match.HasMonth || match.Month != 6 || !match.HasName || match.PostName != "hello-go" {
		t.Fatalf("unexpected match: %+v", match)
	}
}

func TestValidatePostPattern(t *testing.T) {
	if err := ValidatePostPattern("/%postname%%author%"); err == nil {
		t.Fatal("want adjacent variable length tokens to be rejected")
	}
	if err := ValidatePostPattern("/%year%%post_id%.html"); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestTaxonomyPrefix(t *testing.T) {
	SetTaxonomyPrefixes("topics", "labels")
	if got := Category("go"); got != "/topics/go" {
		t.Fatalf("Category()=%q", got)
	}
	if got := Tag("gin"); got != "/labels/gin" {
		t.Fatalf("Tag()=%q", got)
	}
	if slug, ok := ParseCategoryPath("/topics/go"); !ok || slug != "go" {
		t.Fatalf("ParseCategoryPath=%q,%v", slug, ok)
	}
	if slug, ok := ParseTagPath("/labels/gin"); !ok || slug != "gin" {
		t.Fatalf("ParseTagPath=%q,%v", slug, ok)
	}
	if err := ValidateTaxonomyPrefix("bad/path"); err == nil {
		t.Fatal("want invalid taxonomy prefix to fail")
	}
	SetTaxonomyPrefixes("category", "tag")
}
