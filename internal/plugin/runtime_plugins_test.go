package plugin

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledPluginFunctionsCompile(t *testing.T) {
	for _, id := range []string{"saying", "comment-smilies"} {
		id := id
		t.Run(id, func(t *testing.T) {
			p, err := LoadPlugin(filepath.Join("..", "..", "plugins", id))
			if err != nil {
				t.Fatal(err)
			}
			hooks := NewRegistry()
			if _, err := CompileFunctions(p, hooks, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPluginManifestWidgetsAreRegisteredAutomatically(t *testing.T) {
	p, err := LoadPlugin(filepath.Join("..", "..", "plugins", "saying"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Hooks.Filters) != 0 {
		t.Fatalf("plugin manifest should not need to declare widgets.available filter, got %v", p.Hooks.Filters)
	}

	hooks := NewRegistry()
	registerPluginWidgets(hooks, p)

	type widgetDecl struct {
		ID       string
		Label    string
		Source   string
		PluginID string
	}
	filtered := hooks.ApplyFilters(context.Background(), "widgets.available", []widgetDecl{})
	widgets, ok := filtered.([]widgetDecl)
	if !ok {
		t.Fatalf("filtered widgets type = %T", filtered)
	}
	if len(widgets) != 1 {
		t.Fatalf("widgets count = %d, want 1", len(widgets))
	}
	if got := widgets[0]; got.ID != "saying" || got.Source != "plugin:saying" || got.PluginID != "saying" {
		t.Fatalf("registered widget = %+v", got)
	}
}

func TestCommentSmiliesHooksRenderPanelAndContent(t *testing.T) {
	p, err := LoadPlugin(filepath.Join("..", "..", "plugins", "comment-smilies"))
	if err != nil {
		t.Fatal(err)
	}
	hooks := NewRegistry()
	if _, err := CompileFunctions(p, hooks, nil); err != nil {
		t.Fatal(err)
	}

	var panel strings.Builder
	hooks.DoAction(context.Background(), "comment_form.after_textarea", &panel, nil)
	if got := panel.String(); !strings.Contains(got, `class="comment-smilies"`) || !strings.Contains(got, `data-smiley-code="[/微笑]"`) {
		t.Fatalf("smiley panel not rendered: %s", got)
	}

	filtered := hooks.ApplyFilters(context.Background(), "comment.render_html", "hello [/微笑]", nil)
	got, ok := filtered.(string)
	if !ok {
		t.Fatalf("filtered content type = %T", filtered)
	}
	if !strings.Contains(got, `class="comment-smiley"`) || !strings.Contains(got, `/plugin-assets/comment-smilies/smilies/`) {
		t.Fatalf("smiley shortcode not rendered: %s", got)
	}
}
