package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/wenlog/internal/i18n"
	"github.com/youthlin/wenlog/internal/theme"
)

type menuLocationPanel struct {
	Location theme.MenuLocation
	Key      string
	Items    []theme.MenuConfigItem
}

// MenusPage 渲染导航菜单配置页。
func (h *Admin) MenusPage(c *gin.Context) {
	tr := i18n.Get(c)
	t := h.currentTheme(c)
	if t == nil {
		h.serverError(c, nil)
		return
	}
	locations := menuLocations(t)
	pages, err := h.st.PublishedPages(c)
	if err != nil {
		h.serverError(c, err)
		return
	}
	panels := make([]menuLocationPanel, 0, len(locations))
	for _, entry := range locations {
		raw, _ := h.st.GetSetting(c, theme.MenuSettingKey(entry.Key))
		panels = append(panels, menuLocationPanel{Location: entry.MenuLocation, Key: entry.Key, Items: theme.ParseMenuConfig(raw)})
	}
	data := h.base(c, tr.T("菜单"))
	data["CurrentAdminNav"] = "menus"
	data["MenuLocations"] = panels
	data["Pages"] = pages
	if msg := c.Query("notice"); msg != "" {
		data["Notice"] = msg
	}
	c.HTML(http.StatusOK, "admin_menus.gohtml", data)
}

// SaveMenus 保存当前主题声明位置的菜单配置。
func (h *Admin) SaveMenus(c *gin.Context) {
	tr := i18n.Get(c)
	t := h.currentTheme(c)
	if t == nil {
		h.serverError(c, nil)
		return
	}
	for _, entry := range menuLocations(t) {
		items := h.parseMenuForm(c, entry.Key)
		raw, err := theme.MarshalMenuConfig(items)
		if err != nil {
			h.serverError(c, err)
			return
		}
		if err := h.st.SaveSetting(c, theme.MenuSettingKey(entry.Key), raw); err != nil {
			h.serverError(c, err)
			return
		}
	}
	c.Redirect(http.StatusSeeOther, "/admin/menus?notice="+tr.T("菜单已保存"))
}

func (h *Admin) parseMenuForm(c *gin.Context, location string) []theme.MenuConfigItem {
	types := c.PostFormArray("menu_type_" + location)
	items := make([]theme.MenuConfigItem, 0, len(types))
	for i, typ := range types {
		typ = strings.TrimSpace(typ)
		item := theme.MenuConfigItem{
			ID:       strings.TrimSpace(formArrayValue(c, "menu_id_"+location, i)),
			Type:     typ,
			Title:    strings.TrimSpace(formArrayValue(c, "menu_title_"+location, i)),
			URL:      strings.TrimSpace(formArrayValue(c, "menu_url_"+location, i)),
			ParentID: strings.TrimSpace(formArrayValue(c, "menu_parent_"+location, i)),
			Target:   strings.TrimSpace(formArrayValue(c, "menu_target_"+location, i)),
			Order:    i + 1,
		}
		if postID, err := strconv.ParseUint(formArrayValue(c, "menu_post_id_"+location, i), 10, 64); err == nil {
			item.PostID = uint(postID)
		}
		if item.ID == "" && item.Type == theme.MenuItemTypePage {
			item.ID = theme.MenuPageID(item.PostID)
		}
		if item.ID == "" {
			item.ID = newMenuItemID()
		}
		if item.Type == theme.MenuItemTypePage && item.PostID == 0 {
			continue
		}
		if item.Type == theme.MenuItemTypeCustom && (item.Title == "" || item.URL == "") {
			continue
		}
		items = append(items, item)
	}
	return items
}

func menuLocations(t *theme.Theme) theme.MenuLocationEntries {
	if t != nil && len(t.MenuLocations) > 0 {
		return t.MenuLocations
	}
	return theme.MenuLocationEntries{{Key: "primary", MenuLocation: theme.MenuLocation{Name: "主导航", Description: "站点顶部导航菜单"}}}
}

func formArrayValue(c *gin.Context, key string, index int) string {
	values := c.PostFormArray(key)
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func newMenuItemID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "m_" + hex.EncodeToString(b[:])
	}
	return "m_fallback"
}
