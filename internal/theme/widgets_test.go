package theme

import "testing"

func TestResolveWidgetsRequiresWidgetArea(t *testing.T) {
	themeWithoutWidgets := &Theme{}
	if got := ResolveWidgets(`[{
  "id": "search"
}]`, themeWithoutWidgets, "footer"); len(got) != 0 {
		t.Fatalf("theme without widget areas rendered widgets: %+v", got)
	}

	themeWithSearch := &Theme{Widgets: []WidgetDecl{{ID: "search", Area: "sidebar"}}}
	got := ResolveWidgets(`[{
  "id": "search"
}, {
  "id": "recent_comments"
}]`, themeWithSearch, "sidebar")
	if len(got) != 1 || got[0].ID != "search" {
		t.Fatalf("ResolveWidgets()=%+v, want only declared search widget", got)
	}
}

func TestResolveWidgetsAllowsBuiltinsInDeclaredArea(t *testing.T) {
	themeWithoutWidgets := &Theme{WidgetAreas: map[string]WidgetArea{"footer": {Name: "页脚"}}}
	got := ResolveWidgets(`[{
  "id": "search"
}, {
  "id": "recent_posts",
  "opts": {"count": "3"}
}, {
  "id": "theme_only"
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
