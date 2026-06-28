package plugin

import (
	"context"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	root "github.com/youthlin/blog/hook"
)

func TestRenderWidgetUsesActionFirst(t *testing.T) {
	hooks := NewRegistry()
	hooks.AddAction("widget.render", func(ctx context.Context, args ...any) {
		if len(args) < 2 {
			return
		}
		out, _ := args[0].(*strings.Builder)
		renderCtx, _ := args[1].(root.WidgetRenderContext)
		if out == nil || renderCtx.PluginID != "demo" || renderCtx.WidgetID != "hello" {
			return
		}
		out.WriteString("<p>from action</p>")
	}, Source{Type: SourcePlugin, ID: "demo"})
	m := &Manager{
		plugins: map[string]*Plugin{"demo": {ID: "demo", Name: "Demo", Dir: t.TempDir()}},
		hooks:   hooks,
	}

	html, ok := m.RenderWidget(context.Background(), "demo", "hello", nil, nil)
	if !ok || html != template.HTML("<p>from action</p>") {
		t.Fatalf("RenderWidget(action)=(%q,%v), want action html", html, ok)
	}
}

func TestRenderWidgetFallsBackToTemplate(t *testing.T) {
	dir := t.TempDir()
	widgetsDir := filepath.Join(dir, "widgets")
	if err := os.MkdirAll(widgetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(widgetsDir, "hello.gohtml"), []byte(`{{define "widget_hello"}}<section>{{pluginOption "title"}}</section>{{end}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{
		plugins: map[string]*Plugin{"demo": {ID: "demo", Name: "Demo", Dir: dir}},
		hooks:   NewRegistry(),
	}

	html, ok := m.RenderWidget(context.Background(), "demo", "hello", map[string]string{"title": "Hi"}, nil)
	if !ok || html != template.HTML("<section>Hi</section>") {
		t.Fatalf("RenderWidget(template)=(%q,%v), want template html", html, ok)
	}
}

func TestRenderWidgetTemplateCanUseFriendlyPluginDataAPI(t *testing.T) {
	dir := t.TempDir()
	widgetsDir := filepath.Join(dir, "widgets")
	if err := os.MkdirAll(widgetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(widgetsDir, "hello.gohtml"), []byte(`{{define "widget_hello"}}<section>{{pluginData "greeting" "name" (option "name")}}</section>{{end}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	api := root.New(nil, nil, "plugin_demo")
	api.RegisterFunc("greeting", func(args root.Args) any {
		return "Hello, " + args.String("name", "")
	})
	m := &Manager{
		plugins: map[string]*Plugin{"demo": {ID: "demo", Name: "Demo", Dir: dir}},
		hooks:   NewRegistry(),
		scripts: map[string]*FunctionsScript{"demo": {PluginID: "demo", api: api}},
	}

	html, ok := m.RenderWidget(context.Background(), "demo", "hello", map[string]string{"name": "Plugin"}, nil)
	if !ok || html != template.HTML("<section>Hello, Plugin</section>") {
		t.Fatalf("RenderWidget(pluginData)=(%q,%v), want invoked html", html, ok)
	}
}

func TestRegisterFuncSupportsCommonSignatures(t *testing.T) {
	api := root.New(nil, nil, "plugin_demo")
	api.RegisterFunc("args", func(args root.Args) any { return args.Int("n", 0) + 1 })
	api.RegisterFunc("api_args", func(api *root.API, args root.Args) any { return api.Snippet(args.String("text", ""), 2) })
	api.RegisterFunc("legacy", func(api *root.API, args map[string]any) any { return args["name"] })

	ctx := context.Background()
	if got := api.InvokeFunc(ctx, "args", map[string]any{"n": "2"}); got != 3 {
		t.Fatalf("args func = %v, want 3", got)
	}
	if got := api.InvokeFunc(ctx, "api_args", map[string]any{"text": "abcd"}); got != "ab…" {
		t.Fatalf("api_args func = %v, want ab…", got)
	}
	if got := api.InvokeFunc(ctx, "legacy", map[string]any{"name": "old"}); got != "old" {
		t.Fatalf("legacy func = %v, want old", got)
	}
}
