package theme

import "testing"

func TestResolveWidgetsRequiresThemeDeclaration(t *testing.T) {
	themeWithoutWidgets := &Theme{}
	if got := ResolveWidgets(`[{
  "id": "search"
}]`, themeWithoutWidgets, "footer"); len(got) != 0 {
		t.Fatalf("theme without widget declarations rendered widgets: %+v", got)
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
