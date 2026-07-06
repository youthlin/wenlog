package plugin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youthlin/wenlog/hook"
)

type testSettingStore struct{}

func (testSettingStore) GetSetting(context.Context, string) (string, error) { return "", nil }
func (testSettingStore) SetSetting(context.Context, string, string) error   { return nil }

func TestBundledPluginFunctionsCompile(t *testing.T) {
	for _, id := range []string{"common-widgets", "comment-smilies"} {
		id := id
		t.Run(id, func(t *testing.T) {
			p, err := LoadPlugin(filepath.Join("..", "..", "web", "plugins", id))
			if err != nil {
				t.Fatal(err)
			}
			hooks := NewRegistry()
			if _, err := CompileFunctions(context.Background(), p, hooks, nil); err != nil {
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
	p := m.Get("common-widgets")
	if p == nil {
		t.Fatal("common-widgets plugin not loaded")
	}
	if len(p.Hooks.Filters) != 0 {
		t.Fatalf("plugin manifest should not need to declare widget filter, got %v", p.Hooks.Filters)
	}

	widgets := m.EnabledWidgetDecls(context.Background())
	if len(widgets) != 2 {
		t.Fatalf("widgets count = %d, want 2", len(widgets))
	}
	if got := widgets[0]; got.ID != "saying" || got.Source != "plugin" || got.PluginID != "common-widgets" {
		t.Fatalf("registered widget = %+v", got)
	}
}

type countingSettingStore struct {
	value    string
	getCount int
	setCount int
}

func (s *countingSettingStore) GetSetting(context.Context, string) (string, error) {
	s.getCount++
	return s.value, nil
}

func (s *countingSettingStore) SetSetting(_ context.Context, _ string, value string) error {
	s.setCount++
	s.value = value
	return nil
}

func TestManagerEnabledIDsUsesMemoryCache(t *testing.T) {
	stored, err := json.Marshal([]string{"common-widgets"})
	if err != nil {
		t.Fatal(err)
	}
	settings := &countingSettingStore{value: string(stored)}
	m, err := NewManager(filepath.Join("..", "..", "web", "plugins"), settings)
	if err != nil {
		t.Fatal(err)
	}

	ids := m.EnabledIDs(context.Background())
	if got, want := strings.Join(ids, ","), "common-widgets"; got != want {
		t.Fatalf("enabled ids = %q, want %q", got, want)
	}
	ids[0] = "mutated"
	if got, want := strings.Join(m.EnabledIDs(context.Background()), ","), "common-widgets"; got != want {
		t.Fatalf("cached enabled ids should be cloned, got %q want %q", got, want)
	}
	if settings.getCount != 1 {
		t.Fatalf("GetSetting calls = %d, want 1", settings.getCount)
	}

	if err := m.Enable(context.Background(), "comment-smilies"); err != nil {
		t.Fatal(err)
	}
	if settings.setCount != 1 {
		t.Fatalf("SetSetting calls = %d, want 1", settings.setCount)
	}
	if got, want := strings.Join(m.EnabledIDs(context.Background()), ","), "common-widgets,comment-smilies"; got != want {
		t.Fatalf("enabled ids after enable = %q, want %q", got, want)
	}
	if settings.getCount != 1 {
		t.Fatalf("GetSetting calls after cache update = %d, want 1", settings.getCount)
	}
}

func TestCommentSmiliesHooksRenderPanelAndContent(t *testing.T) {
	// 主题模板里直接用这个 slot 名称调用 slot；常量必须和模板/manifest 保持一致，
	// 否则插件虽然成功注册 hook，但评论表单不会渲染表情面板。
	if hook.HookCommentFormAfterTextarea != "comment.form.after_textarea" {
		t.Fatalf("HookCommentFormAfterTextarea = %q, want comment.form.after_textarea", hook.HookCommentFormAfterTextarea)
	}

	p, err := LoadPlugin(filepath.Join("..", "..", "web", "plugins", "comment-smilies"))
	if err != nil {
		t.Fatal(err)
	}
	hooks := NewRegistry()
	if _, err := CompileFunctions(context.Background(), p, hooks, nil); err != nil {
		t.Fatal(err)
	}

	var head strings.Builder
	hooks.DoAction(context.Background(), hook.HookHeadEnd, &head, nil)
	if got := head.String(); !strings.Contains(got, `/plugin-assets/comment-smilies/comment-smilies.css?v=`) {
		t.Fatalf("smiley css not rendered: %s", got)
	}

	var panel strings.Builder
	hooks.DoAction(context.Background(), hook.HookCommentFormAfterTextarea, &panel, nil)
	if got := panel.String(); !strings.Contains(got, `class="comment-smilies"`) || !strings.Contains(got, `class="comment-smilies-label sr-only"`) || !strings.Contains(got, `data-smiley-code="[/微笑]"`) {
		t.Fatalf("smiley panel not rendered: %s", got)
	}

	filtered := hooks.ApplyFilters(context.Background(), hook.HookCommentContentHTML, "hello [/微笑]", nil)
	got, ok := filtered.(string)
	if !ok {
		t.Fatalf("filtered content type = %T", filtered)
	}
	if !strings.Contains(got, `class="comment-smiley"`) || !strings.Contains(got, `/plugin-assets/comment-smilies/smilies/`) {
		t.Fatalf("smiley shortcode not rendered: %s", got)
	}
}
