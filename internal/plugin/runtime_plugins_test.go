package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/model"
	"github.com/youthlin/wenlog/internal/permalink"
	"github.com/youthlin/wenlog/internal/store"
)

type testSettingStore struct{}

func (testSettingStore) GetSetting(context.Context, string) (string, error) { return "", nil }
func (testSettingStore) SetSetting(context.Context, string, string) error   { return nil }

func TestBundledPluginFunctionsCompile(t *testing.T) {
	for _, id := range []string{"common-widgets", "post-comment-enhance"} {
		id := id
		t.Run(id, func(t *testing.T) {
			p, err := LoadPlugin(filepath.Join("..", "..", "web", "plugins", id))
			if err != nil {
				t.Fatal(err)
			}
			hooks := hook.NewRegistry()
			if _, err := CompileFunctions(context.Background(), p, hooks, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompileFunctionsReturnsRegisterError(t *testing.T) {
	p := writeTestPlugin(t, "bad-register", `package plugin

import (
	"errors"
	"hook"
)

func Register(api *hook.API) error {
	return errors.New("register failed")
}
`)
	hooks := hook.NewRegistry()
	_, err := CompileFunctions(context.Background(), p, hooks, nil)
	if err == nil || !strings.Contains(err.Error(), "register failed") {
		t.Fatalf("CompileFunctions error = %v, want register failed", err)
	}
}

func TestCompileFunctionsReturnsRegistrationErrors(t *testing.T) {
	p := writeTestPlugin(t, "bad-hook", `package plugin

import "hook"

func Register(api *hook.API) {
	api.AddFilter("bad.filter", func() any { return "ignored" })
}
`)
	hooks := hook.NewRegistry()
	_, err := CompileFunctions(context.Background(), p, hooks, nil)
	if err == nil || !strings.Contains(err.Error(), "注册 filter[bad.filter]失败") {
		t.Fatalf("CompileFunctions error = %v, want registration error", err)
	}
	if len(hooks.Filters("bad.filter")) != 0 {
		t.Fatalf("invalid filter should not be registered")
	}
}

func TestCompileFunctionsExportsHookViewTypes(t *testing.T) {
	p := writeTestPlugin(t, "view-types", `package plugin

import "hook"

func Register(api *hook.API) error {
	api.AddFilter(hook.Consts.FilterPostTitle, func(value string, post hook.PostView) string {
		return value + ":" + post.Title
	})
	api.AddFilter(hook.Consts.FilterCommentContentHTML, func(value string, comment hook.CommentView) string {
		return value + ":" + comment.Author
	})
	api.AddFilter(hook.Consts.FilterWidgetRenderHTML, func(value string, widget hook.WidgetRenderView) string {
		return value + ":" + widget.ID
	})
	api.AddFilter(hook.Consts.FilterHeadMeta, func(value string, meta hook.HeadMetaView) string {
		return value + ":" + meta.Title
	})
	return api.RegistrationError()
}
`)
	hooks := hook.NewRegistry()
	if _, err := CompileFunctions(context.Background(), p, hooks, nil); err != nil {
		t.Fatalf("CompileFunctions: %v", err)
	}

	cases := []struct {
		name   string
		filter string
		value  string
		arg    any
		want   string
	}{
		{
			name:   "post view",
			filter: hook.FilterPostTitle,
			value:  "title",
			arg:    hook.PostView{Title: "Hello"},
			want:   "title:Hello",
		},
		{
			name:   "comment view",
			filter: hook.FilterCommentContentHTML,
			value:  "comment",
			arg:    hook.CommentView{Author: "Alice"},
			want:   "comment:Alice",
		},
		{
			name:   "widget render view",
			filter: hook.FilterWidgetRenderHTML,
			value:  "widget",
			arg:    hook.WidgetRenderView{ID: "recent_posts"},
			want:   "widget:recent_posts",
		},
		{
			name:   "head meta view",
			filter: hook.FilterHeadMeta,
			value:  "meta",
			arg:    hook.HeadMetaView{Title: "About"},
			want:   "meta:About",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hooks.ApplyFilters(context.Background(), tc.filter, tc.value, tc.arg)
			if got != tc.want {
				t.Fatalf("ApplyFilters(%s) = %v, want %s", tc.filter, got, tc.want)
			}
		})
	}
}

func writeTestPlugin(t *testing.T, id, functions string) *Plugin {
	t.Helper()
	dir := filepath.Join(t.TempDir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "id: " + id + "\nname: Test Plugin\nversion: 0.0.1\n"
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "functions.goyaegi"), []byte(functions), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPlugin(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadPluginParsesLifecycleDecl(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lifecycle-demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `id: lifecycle-demo
name: Lifecycle Demo
lifecycle:
  activate: true
  deactivate: true
  uninstall: true
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPlugin(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Lifecycle.Activate || !p.Lifecycle.Deactivate || !p.Lifecycle.Uninstall {
		t.Fatalf("lifecycle = %+v, want all true", p.Lifecycle)
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

	if err := m.Enable(context.Background(), "post-comment-enhance"); err != nil {
		t.Fatal(err)
	}
	if settings.setCount != 1 {
		t.Fatalf("SetSetting calls = %d, want 1", settings.setCount)
	}
	if got, want := strings.Join(m.EnabledIDs(context.Background()), ","), "common-widgets,post-comment-enhance"; got != want {
		t.Fatalf("enabled ids after enable = %q, want %q", got, want)
	}
	if settings.getCount != 1 {
		t.Fatalf("GetSetting calls after cache update = %d, want 1", settings.getCount)
	}
}

func TestManagerInstallCopiesPluginThroughStaging(t *testing.T) {
	pluginsDir := t.TempDir()
	srcDir := filepath.Join(t.TempDir(), "installable")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "id: installable\nname: Installable\nversion: 0.0.1\n"
	if err := os.WriteFile(filepath.Join(srcDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "asset.txt"), []byte("copied"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(pluginsDir, testSettingStore{})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := m.Install(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	if installed.ID != "installable" || installed.Dir != filepath.Join(pluginsDir, "installable") {
		t.Fatalf("installed plugin = %+v, want target dir plugin", installed)
	}
	if got := m.Get("installable"); got == nil || got.Dir != installed.Dir {
		t.Fatalf("manager plugin = %+v, want installed plugin", got)
	}
	data, err := os.ReadFile(filepath.Join(pluginsDir, "installable", "asset.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "copied" {
		t.Fatalf("copied asset = %q, want copied", data)
	}
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".install-installable-") {
			t.Fatalf("staging dir should be cleaned up, found %s", entry.Name())
		}
	}
}

func TestManagerUninstallDeletesPluginDir(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginDir := filepath.Join(pluginsDir, "removable")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "id: removable\nname: Removable\nversion: 0.0.1\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	settings := &countingSettingStore{}
	m, err := NewManager(pluginsDir, settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Enable(context.Background(), "removable"); err != nil {
		t.Fatal(err)
	}
	if err := m.Uninstall(context.Background(), "removable"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Fatalf("plugin dir should be deleted, stat err=%v", err)
	}
	if m.Get("removable") != nil {
		t.Fatalf("uninstalled plugin should be removed from manager")
	}
	if got := strings.Join(m.EnabledIDs(context.Background()), ","); got != "" {
		t.Fatalf("enabled ids after uninstall = %q, want empty", got)
	}
}

func TestSmiliesHooksRenderPanelAndContent(t *testing.T) {
	// 主题模板通过 do_action 调用这个 action；常量必须和模板/manifest 保持一致，
	// 否则插件虽然成功注册 hook，但评论表单不会渲染表情面板。
	if hook.ActionCommentFormAfterTextarea != "comment.form.after_textarea" {
		t.Fatalf("ActionCommentFormAfterTextarea = %q, want comment.form.after_textarea", hook.ActionCommentFormAfterTextarea)
	}

	p, err := LoadPlugin(filepath.Join("..", "..", "web", "plugins", "post-comment-enhance"))
	if err != nil {
		t.Fatal(err)
	}
	hooks := hook.NewRegistry()
	if _, err := CompileFunctions(context.Background(), p, hooks, nil); err != nil {
		t.Fatal(err)
	}

	var head strings.Builder
	hooks.DoAction(context.Background(), hook.ActionHeadEnd, &head, nil)
	if got := head.String(); !strings.Contains(got, `/plugin-assets/post-comment-enhance/style.css?v=`) {
		t.Fatalf("smiley css not rendered: %s", got)
	}

	var panel strings.Builder
	hooks.DoAction(context.Background(), hook.ActionCommentFormAfterTextarea, &panel, nil)
	if got := panel.String(); !strings.Contains(got, `class="smilies"`) || !strings.Contains(got, `class="smilies-label sr-only"`) || !strings.Contains(got, `data-smiley-code="[/微笑]"`) {
		t.Fatalf("smiley panel not rendered: %s", got)
	}

	filtered := hooks.ApplyFilters(context.Background(), hook.FilterCommentContentHTML, "hello [/微笑]", nil)
	got, ok := filtered.(string)
	if !ok {
		t.Fatalf("filtered content type = %T", filtered)
	}
	if !strings.Contains(got, `class="smiley"`) || !strings.Contains(got, `/plugin-assets/post-comment-enhance/smilies/`) {
		t.Fatalf("smiley shortcode not rendered: %s", got)
	}
}

func TestPostCommentEnhanceFooterPostURLUsesPermalink(t *testing.T) {
	permalink.SetPostPattern("/%year%/%monthnum%/%postname%/")
	t.Cleanup(func() { permalink.SetPostPattern(consts.SettingsPostPermalinkDefault) })

	p, err := LoadPlugin(filepath.Join("..", "..", "web", "plugins", "post-comment-enhance"))
	if err != nil {
		t.Fatal(err)
	}
	hooks := hook.NewRegistry()
	if _, err := CompileFunctions(context.Background(), p, hooks, nil); err != nil {
		t.Fatal(err)
	}

	loader := &store.DataLoader{Settings: map[string]string{
		consts.SettingsSiteURL:                         "https://example.test/",
		"plugin_post-comment-enhance_post_footer_html": `<a href="%postURL%">%postTitle%</a>`,
	}}
	ctx := hook.WithDataLoader(context.Background(), loader)
	post := hook.PostView{
		ID:          42,
		Title:       "Hello",
		Slug:        "hello-go",
		PostType:    model.PostTypePost,
		PublishedAt: time.Date(2026, 6, 11, 9, 8, 7, 0, time.UTC),
	}

	filtered := hooks.ApplyFilters(ctx, hook.FilterPostFooterHTML, "<p>body</p>", post)
	got, ok := filtered.(string)
	if !ok {
		t.Fatalf("filtered content type = %T", filtered)
	}
	if !strings.Contains(got, `href="https://example.test/2026/06/hello-go/"`) {
		t.Fatalf("post footer should use permalink URL, got: %s", got)
	}
	if strings.Contains(got, "/?p=42") {
		t.Fatalf("post footer should not use short post ID URL, got: %s", got)
	}
}
