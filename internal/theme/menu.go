package theme

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/youthlin/wenlog/internal/model"
	"github.com/youthlin/wenlog/internal/permalink"
)

const MenuItemTypePage = "page"
const MenuItemTypeCustom = "custom"

// MenuConfigItem 是后台保存的菜单配置项，采用扁平结构便于表单编辑。
type MenuConfigItem struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	PostID   uint   `json:"post_id,omitempty"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
	Target   string `json:"target,omitempty"`
	Order    int    `json:"order,omitempty"`
}

// MenuItem 是模板渲染用的菜单树节点。
type MenuItem struct {
	ID       string
	Title    string
	URL      string
	Target   string
	Children []MenuItem
}

func MenuSettingKey(location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		location = "primary"
	}
	return "menu_" + location
}

func ParseMenuConfig(raw string) []MenuConfigItem {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var items []MenuConfigItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	return normalizeMenuConfig(items)
}

func MarshalMenuConfig(items []MenuConfigItem) (string, error) {
	items = normalizeMenuConfig(items)
	data, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func ResolveMenuItems(raw string, fallbackPages []model.Post) []MenuItem {
	if strings.TrimSpace(raw) == "[]" {
		return nil
	}
	items := ParseMenuConfig(raw)
	if strings.TrimSpace(raw) == "" {
		return MenuItemsFromPages(fallbackPages)
	}
	if len(items) == 0 {
		return nil
	}
	pages := make(map[uint]model.Post, len(fallbackPages))
	for _, p := range fallbackPages {
		pages[p.ID] = p
	}
	return BuildMenuTree(items, pages)
}

func MenuItemsFromPages(pages []model.Post) []MenuItem {
	result := make([]MenuItem, 0, len(pages))
	for _, p := range pages {
		if p.Title == "" {
			continue
		}
		result = append(result, MenuItem{ID: menuPageID(p.ID), Title: p.Title, URL: permalink.Post(&p)})
	}
	return result
}

func BuildMenuTree(config []MenuConfigItem, pages map[uint]model.Post) []MenuItem {
	config = normalizeMenuConfig(config)
	nodes := make(map[string]*MenuItem, len(config))
	parents := make(map[string]string, len(config))
	for _, item := range config {
		node, ok := menuNode(item, pages)
		if !ok {
			continue
		}
		nodes[item.ID] = &node
		parents[item.ID] = item.ParentID
	}
	var roots []*MenuItem
	for _, item := range config {
		node := nodes[item.ID]
		if node == nil {
			continue
		}
		parentID := parents[item.ID]
		parent := nodes[parentID]
		if parentID == "" || parent == nil || parentID == item.ID || wouldCreateMenuCycle(parents, item.ID, parentID) {
			roots = append(roots, node)
			continue
		}
		parent.Children = append(parent.Children, *node)
	}
	result := make([]MenuItem, 0, len(roots))
	for _, root := range roots {
		result = append(result, *root)
	}
	return result
}

func wouldCreateMenuCycle(parents map[string]string, id, parentID string) bool {
	seen := map[string]bool{id: true}
	for parentID != "" {
		if seen[parentID] {
			return true
		}
		seen[parentID] = true
		parentID = parents[parentID]
	}
	return false
}

func menuNode(item MenuConfigItem, pages map[uint]model.Post) (MenuItem, bool) {
	switch item.Type {
	case MenuItemTypePage:
		p, ok := pages[item.PostID]
		if !ok {
			return MenuItem{}, false
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = p.Title
		}
		return MenuItem{ID: item.ID, Title: title, URL: permalink.Post(&p), Target: item.Target}, true
	case MenuItemTypeCustom:
		title := strings.TrimSpace(item.Title)
		url := strings.TrimSpace(item.URL)
		if title == "" || url == "" {
			return MenuItem{}, false
		}
		return MenuItem{ID: item.ID, Title: title, URL: url, Target: item.Target}, true
	default:
		return MenuItem{}, false
	}
}

func normalizeMenuConfig(items []MenuConfigItem) []MenuConfigItem {
	out := make([]MenuConfigItem, 0, len(items))
	idMap := make(map[string]string)
	for i, item := range items {
		oldID := strings.TrimSpace(item.ID)
		item.ID = strings.TrimSpace(item.ID)
		item.Type = strings.TrimSpace(item.Type)
		item.Title = strings.TrimSpace(item.Title)
		item.URL = strings.TrimSpace(item.URL)
		item.ParentID = strings.TrimSpace(item.ParentID)
		item.Target = strings.TrimSpace(item.Target)
		if item.Order == 0 {
			item.Order = i + 1
		}
		if item.Type == MenuItemTypePage && item.PostID > 0 {
			item.ID = menuPageID(item.PostID)
		} else if item.ID == "" && item.PostID > 0 {
			item.ID = menuPageID(item.PostID)
		}
		if item.ID == "" {
			continue
		}
		if oldID != "" && oldID != item.ID {
			idMap[oldID] = item.ID
		}
		out = append(out, item)
	}
	if len(idMap) > 0 {
		for i := range out {
			if mapped := idMap[out[i].ParentID]; mapped != "" {
				out[i].ParentID = mapped
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

func MenuPageID(id uint) string {
	if id == 0 {
		return ""
	}
	return "page_" + strconv.FormatUint(uint64(id), 10)
}

func menuPageID(id uint) string { return MenuPageID(id) }
