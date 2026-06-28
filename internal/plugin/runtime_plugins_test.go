package plugin

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	root "github.com/youthlin/blog/hook"
)

type testSettingStore struct{}

func (testSettingStore) GetSetting(context.Context, string) (string, error) { return "", nil }
func (testSettingStore) SetSetting(context.Context, string, string) error   { return nil }

func TestBundledPluginFunctionsCompile(t *testing.T) {
	for _, id := range []string{"saying", "comment-smilies"} {
		id := id
		t.Run(id, func(t *testing.T) {
			p, err := LoadPlugin(filepath.Join("..", "..", "web", "plugins", id))
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
	m, err := NewManager(filepath.Join("..", "..", "web", "plugins"), testSettingStore{})
	if err != nil {
		t.Fatal(err)
	}
	p := m.Get("saying")
	if p == nil {
		t.Fatal("saying plugin not loaded")
	}
	if len(p.Hooks.Filters) != 0 {
		t.Fatalf("plugin manifest should not need to declare widget filter, got %v", p.Hooks.Filters)
	}

	widgets := m.EnabledWidgetDecls(context.Background())
	if len(widgets) != 1 {
		t.Fatalf("widgets count = %d, want 1", len(widgets))
	}
	if got := widgets[0]; got.ID != "saying" || got.Source != "plugin:saying" {
		t.Fatalf("registered widget = %+v", got)
	}
}

func TestCommentSmiliesHooksRenderPanelAndContent(t *testing.T) {
	// 主题模板里直接用这个 slot 名称调用 slot；常量必须和模板/manifest 保持一致，
	// 否则插件虽然成功注册 hook，但评论表单不会渲染表情面板。
	if root.HookCommentFormAfterTextarea != "comment.form.after_textarea" {
		t.Fatalf("HookCommentFormAfterTextarea = %q, want comment.form.after_textarea", root.HookCommentFormAfterTextarea)
	}

	p, err := LoadPlugin(filepath.Join("..", "..", "web", "plugins", "comment-smilies"))
	if err != nil {
		t.Fatal(err)
	}
	hooks := NewRegistry()
	if _, err := CompileFunctions(p, hooks, nil); err != nil {
		t.Fatal(err)
	}

	var panel strings.Builder
	hooks.DoAction(context.Background(), root.HookCommentFormAfterTextarea, &panel, nil)
	if got := panel.String(); !strings.Contains(got, `class="comment-smilies"`) || !strings.Contains(got, `class="comment-smilies-label sr-only"`) || !strings.Contains(got, `data-smiley-code="[/微笑]"`) {
		t.Fatalf("smiley panel not rendered: %s", got)
	}

	filtered := hooks.ApplyFilters(context.Background(), root.HookCommentContentHTML, "hello [/微笑]", nil)
	got, ok := filtered.(string)
	if !ok {
		t.Fatalf("filtered content type = %T", filtered)
	}
	if !strings.Contains(got, `class="comment-smiley"`) || !strings.Contains(got, `/plugin-assets/comment-smilies/smilies/`) {
		t.Fatalf("smiley shortcode not rendered: %s", got)
	}
}
