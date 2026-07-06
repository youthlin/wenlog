package theme

import (
	"context"
	"testing"
)

func TestWidgetDeclsWithPluginsAllowsPluginWidget(t *testing.T) {
	theme := &Theme{WidgetAreas: map[string]WidgetArea{"sidebar": {Name: "侧栏"}}}

	decls := WidgetDeclsWithPlugins(theme, []WidgetDecl{{ID: "saying", Label: "博主动态", Source: "plugin", PluginID: "common-widgets"}})
	widgets := ResolveWidgetsWithDecls(context.Background(), `[{
  "id": "saying",
  "source": "plugin:common-widgets",
  "opts": {"count": "5"}
}]`, theme, "sidebar", decls)
	if len(widgets) != 1 {
		t.Fatalf("ResolveWidgetsWithDecls(plugin)=%+v, want one plugin widget", widgets)
	}
	if widgets[0].ID != "saying" || widgets[0].Source != WidgetSourcePlugin || widgets[0].PluginID != "common-widgets" || widgets[0].Options["count"] != "5" {
		t.Fatalf("plugin widget=%+v, want saying plugin widget with options", widgets[0])
	}
}

func TestResolveWidgetsLegacyConfigWithoutSourceIsIgnored(t *testing.T) {
	theme := &Theme{WidgetAreas: map[string]WidgetArea{"sidebar": {Name: "侧栏"}}}
	decls := append(WidgetDeclsWithBuiltins(theme), WidgetDecl{ID: "search", Label: "插件搜索", Source: "plugin:plugin-search"})

	widgets := ResolveWidgetsWithDecls(context.Background(), `[{"id":"search"}]`, theme, "sidebar", decls)
	if len(widgets) != 0 {
		t.Fatalf("ResolveWidgetsWithDecls(legacy)=%+v, want none", widgets)
	}
	if !HasLegacyWidgetConfig(`[{"id":"search"}]`) {
		t.Fatalf("HasLegacyWidgetConfig()=false, want true")
	}
}

func TestResolveWidgetsExplicitSourceDoesNotFallback(t *testing.T) {
	theme := &Theme{WidgetAreas: map[string]WidgetArea{"sidebar": {Name: "侧栏"}}}

	widgets := ResolveWidgetsWithDecls(context.Background(), `[{"id":"search","source":"plugin:missing"}]`, theme, "sidebar", WidgetDeclsWithBuiltins(theme))
	if len(widgets) != 0 {
		t.Fatalf("ResolveWidgetsWithDecls(explicit missing source)=%+v, want none", widgets)
	}
}

func TestResolveWidgetsKeepsSameIDPluginWidgetsDistinct(t *testing.T) {
	theme := &Theme{WidgetAreas: map[string]WidgetArea{"sidebar": {Name: "侧栏"}}}
	decls := []WidgetDecl{
		{ID: "weather", Label: "天气 A", Source: "plugin", PluginID: "a"},
		{ID: "weather", Label: "天气 B", Source: "plugin", PluginID: "b"},
	}

	widgets := ResolveWidgetsWithDecls(context.Background(), `[
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
