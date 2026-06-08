package web_test

import (
	"io/fs"
	"testing"

	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/web"
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
