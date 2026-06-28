package theme

import (
	"testing"

	"github.com/youthlin/blog/internal/model"
)

func TestResolveMenuItemsBuildsNestedPageAndCustomItems(t *testing.T) {
	pages := []model.Post{{ID: 1, Title: "关于", Slug: "about", PostType: model.PostTypePage}}
	raw, err := MarshalMenuConfig([]MenuConfigItem{
		{ID: "page_1", Type: MenuItemTypePage, PostID: 1, Order: 1},
		{ID: "custom_1", Type: MenuItemTypeCustom, Title: "GitHub", URL: "https://github.com", ParentID: "page_1", Target: "_blank", Order: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	items := ResolveMenuItems(raw, pages)
	if len(items) != 1 || items[0].Title != "关于" || items[0].URL != "/about" {
		t.Fatalf("root items=%+v, want about page", items)
	}
	if len(items[0].Children) != 1 || items[0].Children[0].URL != "https://github.com" || items[0].Children[0].Target != "_blank" {
		t.Fatalf("children=%+v, want custom child", items[0].Children)
	}
}

func TestResolveMenuItemsFallbackAndEmptyOverride(t *testing.T) {
	pages := []model.Post{{ID: 1, Title: "关于", Slug: "about", PostType: model.PostTypePage}}
	if got := ResolveMenuItems("", pages); len(got) != 1 || got[0].URL != "/about" {
		t.Fatalf("ResolveMenuItems(empty)=%+v, want fallback pages", got)
	}
	if got := ResolveMenuItems("[]", pages); len(got) != 0 {
		t.Fatalf("ResolveMenuItems([])=%+v, want explicit empty menu", got)
	}
}
