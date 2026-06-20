package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/theme"
)

// WidgetsPage 组件配置页。
func (h *Admin) WidgetsPage(c *gin.Context) {
	tr := i18n.Get(c)
	t := h.currentTheme(c)
	if t == nil {
		h.serverError(c, nil)
		return
	}

	// 按区域分组
	type areaData struct {
		Area    theme.WidgetArea
		Widgets []theme.WidgetDecl
		Config  []string // 用户当前配置的组件 ID 列表
		Missing []string // 配置中存在但当前主题未声明的
	}
	var areas []areaData
	for areaKey, area := range t.WidgetAreas {
		config := h.getWidgetConfig(c, areaKey)
		missing := theme.MissingWidgets(config, t)
		var widgets []theme.WidgetDecl
		for _, w := range t.Widgets {
			if w.Area == areaKey {
				widgets = append(widgets, w)
			}
		}
		areas = append(areas, areaData{
			Area:    area,
			Widgets: widgets,
			Config:  parseWidgetConfig(config),
			Missing: missing,
		})
	}

	data := h.base(c, tr.T("组件管理"))
	data["Areas"] = areas
	data["CurrentAdminNav"] = "widgets"
	c.HTML(http.StatusOK, "admin_widgets.gohtml", data)
}

// SaveWidgets 保存组件配置。
func (h *Admin) SaveWidgets(c *gin.Context) {
	tr := i18n.Get(c)
	t := h.currentTheme(c)
	if t == nil {
		h.serverError(c, nil)
		return
	}

	for areaKey := range t.WidgetAreas {
		ids := c.PostFormArray("widgets_" + areaKey)
		if len(ids) == 0 {
			// 空数组表示恢复默认
			if err := h.st.DeleteSetting(c, "widget_"+areaKey); err != nil {
				h.serverError(c, err)
				return
			}
			continue
		}
		data, err := json.Marshal(ids)
		if err != nil {
			h.serverError(c, err)
			return
		}
		if err := h.st.SaveSetting(c, "widget_"+areaKey, string(data)); err != nil {
			h.serverError(c, err)
			return
		}
	}

	c.Redirect(http.StatusSeeOther, "/admin/widgets?notice="+tr.T("组件配置已保存"))
}

func (h *Admin) getWidgetConfig(c *gin.Context, area string) string {
	v, err := h.st.GetSetting(c, "widget_"+area)
	if err != nil || v == "" {
		return ""
	}
	return v
}

func parseWidgetConfig(jsonStr string) []string {
	if jsonStr == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(jsonStr), &ids); err != nil {
		return nil
	}
	return ids
}
