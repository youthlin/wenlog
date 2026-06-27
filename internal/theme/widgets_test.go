package theme

import (
	"context"
	"testing"

	"github.com/youthlin/blog/internal/plugin"
)

func TestResolveWidgetsRequiresWidgetArea(t *testing.T) {
	themeWithoutWidgets := &Theme{}
	if got := ResolveWidgets(`[{
  "id": "search"
}]`, themeWithoutWidgets, "footer"); len(got) != 0 {
		t.Fatalf("theme without widget areas rendered widgets: %+v", got)
	}

	themeWithSearch := &Theme{Widgets: []WidgetDecl{{ID: "search", Area: "sidebar"}}}
	got := ResolveWidgets(`[{
  "id": "search",
  "source": "theme"
}, {
  "id": "recent_comments",
  "source": "builtin"
}]`, themeWithSearch, "sidebar")
	if len(got) != 1 || got[0].ID != "search" {
		t.Fatalf("ResolveWidgets()=%+v, want only declared search widget", got)
	}
}

func TestResolveWidgetsAllowsBuiltinsInDeclaredArea(t *testing.T) {
	themeWithoutWidgets := &Theme{WidgetAreas: map[string]WidgetArea{"footer": {Name: "页脚"}}}
	got := ResolveWidgets(`[{
  "id": "search",
  "source": "builtin"
}, {
  "id": "recent_posts",
  "source": "builtin",
  "opts": {"count": "3"}
}, {
  "id": "theme_only",
  "source": "theme"
}]`, themeWithoutWidgets, "footer")
	if len(got) != 2 || got[0].ID != "search" || got[1].ID != "recent_posts" || got[1].Options["count"] != "3" {
		t.Fatalf("ResolveWidgets(builtin)=%+v, want configured builtins only", got)
	}
}

func TestResolveWidgetsDefaultUsesDeclaredAreaOnly(t *testing.T) {
	theme := &Theme{Widgets: []WidgetDecl{
		{ID: "search", Area: "sidebar"},
		{ID: "recent_posts", Area: "footer"},
	}}
	got := ResolveWidgets("", theme, "sidebar")
	if len(got) != 1 || got[0].ID != "search" {
		t.Fatalf("ResolveWidgets(default)=%+v, want only sidebar widget", got)
	}
}

func TestWidgetDeclsWithFilterAllowsPluginWidget(t *testing.T) {
	theme := &Theme{WidgetAreas: map[string]WidgetArea{"sidebar": {Name: "侧栏"}}}
	hooks := plugin.NewRegistry()
	hooks.AddFilter("widgets.available", func(ctx context.Context, value any, args ...any) any {
		decls := value.([]WidgetDecl)
		return append(decls, WidgetDecl{ID: "saying", Label: "博主动态", Source: "plugin:saying"})
	}, plugin.Source{Type: plugin.SourcePlugin, ID: "saying"})

	decls := WidgetDeclsWithFilter(context.Background(), hooks, theme, "sidebar")
	widgets := ResolveWidgetsWithDecls(`[{
  "id": "saying",
  "source": "plugin:saying",
  "opts": {"count": "5"}
}]`, theme, "sidebar", decls)
	if len(widgets) != 1 {
		t.Fatalf("ResolveWidgetsWithDecls(plugin)=%+v, want one plugin widget", widgets)
	}
	if widgets[0].ID != "saying" || widgets[0].Source != WidgetSourcePlugin || widgets[0].PluginID != "saying" || widgets[0].Options["count"] != "5" {
		t.Fatalf("plugin widget=%+v, want saying plugin widget with options", widgets[0])
	}
}

func TestResolveWidgetsLegacyConfigWithoutSourceIsIgnored(t *testing.T) {
	theme := &Theme{WidgetAreas: map[string]WidgetArea{"sidebar": {Name: "侧栏"}}}
	decls := append(WidgetDeclsWithBuiltins(theme), WidgetDecl{ID: "search", Label: "插件搜索", Source: "plugin:plugin-search"})

	widgets := ResolveWidgetsWithDecls(`[{"id":"search"}]`, theme, "sidebar", decls)
	if len(widgets) != 0 {
		t.Fatalf("ResolveWidgetsWithDecls(legacy)=%+v, want none", widgets)
	}
	if !HasLegacyWidgetConfig(`[{"id":"search"}]`) {
		t.Fatalf("HasLegacyWidgetConfig()=false, want true")
	}
}

func TestResolveWidgetsExplicitSourceDoesNotFallback(t *testing.T) {
	theme := &Theme{WidgetAreas: map[string]WidgetArea{"sidebar": {Name: "侧栏"}}}

	widgets := ResolveWidgetsWithDecls(`[{"id":"search","source":"plugin:missing"}]`, theme, "sidebar", WidgetDeclsWithBuiltins(theme))
	if len(widgets) != 0 {
		t.Fatalf("ResolveWidgetsWithDecls(explicit missing source)=%+v, want none", widgets)
	}
}

func TestResolveWidgetsKeepsSameIDPluginWidgetsDistinct(t *testing.T) {
	theme := &Theme{WidgetAreas: map[string]WidgetArea{"sidebar": {Name: "侧栏"}}}
	decls := []WidgetDecl{
		{ID: "weather", Label: "天气 A", Source: "plugin:a"},
		{ID: "weather", Label: "天气 B", Source: "plugin:b"},
	}

	widgets := ResolveWidgetsWithDecls(`[
		{"id":"weather","source":"plugin:a"},
		{"id":"weather","source":"plugin:b"}
	]`, theme, "sidebar", decls)
	if len(widgets) != 2 {
		t.Fatalf("ResolveWidgetsWithDecls(same id plugins)=%+v, want two widgets", widgets)
	}
	if widgets[0].PluginID != "a" || widgets[1].PluginID != "b" {
		t.Fatalf("plugin widgets=%+v, want plugin ids a and b", widgets)
	}
}
