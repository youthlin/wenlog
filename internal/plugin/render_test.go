package plugin

import (
	"context"
	"html/template"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/youthlin/wenlog/hook"
)

type stubHookInvoker struct {
	called bool
	args   map[string]any
}

func (s *stubHookInvoker) InvokeFunc(_ context.Context, name string, args map[string]any) any {
	s.called = true
	s.args = args
	return "called:" + name
}

func TestRenderWidgetUsesTemplateRenderer(t *testing.T) {
	dir := t.TempDir()
	widgetsDir := filepath.Join(dir, "widgets")
	if err := os.MkdirAll(widgetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(widgetsDir, "hello.gohtml"), []byte(`{{define "widget_hello"}}<section>{{plugin_option "title"}}</section>{{end}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	hooks := hook.NewRegistry()
	hooks.AddAction("widget.render", func(ctx context.Context, args ...any) {
		out := hook.GetActionWriter(ctx)
		if out != nil {
			t.Fatal("legacy widget.render action should not be called")
		}
	}, hook.Source{Type: hook.SourcePlugin, ID: "demo"})
	m := &Manager{
		log:     slog.Default().With("component", "plugin-manager"),
		plugins: map[string]*Plugin{"demo": {ID: "demo", Name: "Demo", Dir: dir}},
		hooks:   hooks,
	}

	html, ok := m.RenderWidget(context.Background(), "demo", "hello", map[string]string{"title": "Hi"}, nil)
	if !ok || html != template.HTML("<section>Hi</section>") {
		t.Fatalf("RenderWidget(template)=(%q,%v), want template html", html, ok)
	}
}

func TestRenderWidgetMissingTemplateReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{
		log:     slog.Default().With("component", "plugin-manager"),
		plugins: map[string]*Plugin{"demo": {ID: "demo", Name: "Demo", Dir: dir}},
		hooks:   hook.NewRegistry(),
	}

	html, ok := m.RenderWidget(context.Background(), "demo", "hello", map[string]string{"title": "Hi"}, nil)
	if ok || html != "" {
		t.Fatalf("RenderWidget(missing template)=(%q,%v), want empty false", html, ok)
	}
}

func TestRenderWidgetTemplateCanUseFriendlyPluginFuncAPI(t *testing.T) {
	dir := t.TempDir()
	widgetsDir := filepath.Join(dir, "widgets")
	if err := os.MkdirAll(widgetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(widgetsDir, "hello.gohtml"), []byte(
		`{{define "widget_hello"}}<section>{{hook_invoke "greeting" "name" (plugin_option "name")}}</section>{{end}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	api := hook.NewAPI().WithDomain("plugin_demo")
	api.RegisterFunc("greeting", func(api *hook.API, args hook.Args) any {
		return "Hello, " + args.String("name", "")
	})
	m := &Manager{
		log:     slog.Default().With("component", "plugin-manager"),
		plugins: map[string]*Plugin{"demo": {ID: "demo", Name: "Demo", Dir: dir}},
		hooks:   hook.NewRegistry(),
		scripts: map[string]*FunctionsScript{"demo": {PluginID: "demo", api: api}},
	}

	html, ok := m.RenderWidget(context.Background(), "demo", "hello", map[string]string{"name": "Plugin"}, nil)
	if !ok || html != template.HTML("<section>Hello, Plugin</section>") {
		t.Fatalf("RenderWidget=(%q,%v), want invoked html", html, ok)
	}
}

func TestPluginTemplateFuncsOnlyNeedHookInvoker(t *testing.T) {
	invoker := &stubHookInvoker{}
	funcs := pluginTemplateFuncs(context.Background(), map[string]string{"name": "Plugin"}, invoker)
	call, ok := funcs["hook_invoke"].(func(string, ...any) any)
	if !ok {
		t.Fatalf("hook_invoke type = %T, want func(string, ...any) any", funcs["hook_invoke"])
	}

	if got := call("greeting", "name", "Plugin"); got != "called:greeting" {
		t.Fatalf("hook_invoke = %v, want called:greeting", got)
	}
	if !invoker.called || invoker.args["name"] != "Plugin" {
		t.Fatalf("invoker args = %#v, called=%v", invoker.args, invoker.called)
	}
}

func TestRegisterFuncSupportsCommonSignatures(t *testing.T) {
	api := hook.NewAPI().WithDomain("plugin_demo")
	api.RegisterFunc("args", func(api *hook.API, args hook.Args) any { return args.Int("n", 0) + 1 })
	api.RegisterFunc("api_args", func(api *hook.API, args hook.Args) any { return api.Snippet(args.String("text", ""), 2) })
	api.RegisterFunc("legacy", func(api *hook.API, args hook.Args) any { return args["name"] })

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
