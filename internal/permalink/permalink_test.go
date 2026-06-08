package permalink

import (
	"testing"
	"time"

	"github.com/youthlin/blog/internal/model"
)

func TestPost(t *testing.T) {
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
		year, id, ok := ParsePostPath(tt.path)
		if ok != tt.wantOK || (ok && (year != tt.wantYear || id != tt.wantID)) {
			t.Errorf("ParsePostPath(%q) = (%d,%d,%v), want (%d,%d,%v)",
				tt.path, year, id, ok, tt.wantYear, tt.wantID, tt.wantOK)
		}
	}
}
