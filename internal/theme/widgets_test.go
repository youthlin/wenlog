package theme

import (
	"testing"
)

func TestResolveWidgetsRequiresWidgetArea(t *testing.T) {
	themeWithoutWidgets := &Theme{}
	if got := ResolveWidgets(`[{
  "id": "search"
}]`, themeWithoutWidgets, "footer"); len(got) != 0 {
		t.Fatalf("theme without widget areas rendered widgets: %+v", got)
	}

	themeWithSearch := &Theme{
		WidgetAreas: map[string]WidgetArea{"sidebar": {Name: "侧栏"}},
		Widgets:     []WidgetDecl{{ID: "search"}},
	}
	got := ResolveWidgets(`[{
  "id": "search",
  "source": "theme"
}, {
  "id": "recent_comments",
  "source": "builtin"
}]`, themeWithSearch, "sidebar")
	if len(got) != 2 || got[0].ID != "search" || got[1].ID != "recent_comments" {
		t.Fatalf("ResolveWidgets()=%+v, want search and recent_comments", got)
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

func TestResolveWidgetsEmptyConfigRendersNoWidgets(t *testing.T) {
	theme := &Theme{Widgets: []WidgetDecl{
		{ID: "search"},
		{ID: "recent_posts"},
	}}
	got := ResolveWidgets("", theme, "sidebar")
	if len(got) != 0 {
		t.Fatalf("ResolveWidgets(empty config)=%+v, want no widgets", got)
	}
}

func TestWidgetDeclsWithPluginsAllowsPluginWidget(t *testing.T) {
	theme := &Theme{WidgetAreas: map[string]WidgetArea{"sidebar": {Name: "侧栏"}}}

	decls := WidgetDeclsWithPlugins(theme, []WidgetDecl{{ID: "saying", Label: "博主动态", Source: "plugin", PluginID: "common-widgets"}})
	widgets := ResolveWidgetsWithDecls(`[{
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
		{ID: "weather", Label: "天气 A", Source: "plugin", PluginID: "a"},
		{ID: "weather", Label: "天气 B", Source: "plugin", PluginID: "b"},
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
