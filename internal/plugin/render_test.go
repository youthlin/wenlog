package plugin

import (
	"context"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	root "github.com/youthlin/blog/pluginapi"
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

func TestRenderWidgetTemplateCanInvokePluginFunc(t *testing.T) {
	dir := t.TempDir()
	widgetsDir := filepath.Join(dir, "widgets")
	if err := os.MkdirAll(widgetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(widgetsDir, "hello.gohtml"), []byte(`{{define "widget_hello"}}<section>{{pluginInvoke "greeting" "name" (pluginOption "name")}}</section>{{end}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	api := root.New(nil, nil, "plugin_demo")
	api.RegisterFunc("greeting", func(api *root.API, args map[string]any) any {
		return "Hello, " + args["name"].(string)
	})
	m := &Manager{
		plugins: map[string]*Plugin{"demo": {ID: "demo", Name: "Demo", Dir: dir}},
		hooks:   NewRegistry(),
		scripts: map[string]*FunctionsScript{"demo": {PluginID: "demo", api: api}},
	}

	html, ok := m.RenderWidget(context.Background(), "demo", "hello", map[string]string{"name": "Plugin"}, nil)
	if !ok || html != template.HTML("<section>Hello, Plugin</section>") {
		t.Fatalf("RenderWidget(pluginInvoke)=(%q,%v), want invoked html", html, ok)
	}
}
