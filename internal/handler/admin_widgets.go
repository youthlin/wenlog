package handler

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/theme"
)

// configuredWidget 是模板中一个已配置组件的展示数据。
type configuredWidget struct {
	ID    string            // 组件 ID
	Label string            // 中文显示名
	Decl  theme.WidgetDecl  // 主题声明（含选项定义）
	Opts  map[string]string // 当前选项值
	Index int               // 在区域内的序号
}

// areaPanel 是模板中一个区域面板的展示数据。
type areaPanel struct {
	Area              theme.WidgetArea
	AreaKey           string
	ConfiguredWidgets []configuredWidget
}

// WidgetsPage 组件配置页。
func (h *Admin) WidgetsPage(c *gin.Context) {
	tr := i18n.Get(c)
	t := h.currentTheme(c)
	if t == nil {
		h.serverError(c, nil)
		return
	}

	// 构建 widgetDecl 查找表
	declByID := make(map[string]theme.WidgetDecl)
	for _, w := range t.Widgets {
		declByID[w.ID] = w
	}

	// 可用组件列表（用于"添加"下拉）
	type availWidget struct {
		ID    string
		Label string
		Area  string // 默认区域
	}
	var availableWidgets []availWidget
	for _, w := range t.Widgets {
		label := w.Label
		if label == "" {
			label = w.ID
		}
		availableWidgets = append(availableWidgets, availWidget{
			ID:    w.ID,
			Label: label,
			Area:  w.Area,
		})
	}

	// 按区域构建面板
	var areas []areaPanel
	for areaKey, area := range t.WidgetAreas {
		configJSON := h.getWidgetConfig(c, areaKey)
		items := theme.ParseWidgetConfig(configJSON)

		var configured []configuredWidget
		for i, item := range items {
			decl, ok := declByID[item.ID]
			if !ok {
				// 配置中存在但主题未声明，跳过
				continue
			}
			label := decl.Label
			if label == "" {
				label = item.ID
			}
			opts := item.Opts
			if opts == nil {
				opts = make(map[string]string)
			}
			configured = append(configured, configuredWidget{
				ID:    item.ID,
				Label: label,
				Decl:  decl,
				Opts:  opts,
				Index: i,
			})
		}

		areas = append(areas, areaPanel{
			Area:              area,
			AreaKey:           areaKey,
			ConfiguredWidgets: configured,
		})
	}

	// 构建完整组件声明 JSON（含 Options），供 JS 动态构建选项表单
	widgetDeclsJSON, _ := json.Marshal(t.Widgets)

	data := h.base(c, tr.T("组件管理"))
	data["AvailableWidgets"] = availableWidgets
	data["Areas"] = areas
	data["WidgetDeclsJSON"] = template.JS(widgetDeclsJSON)
	data["CurrentAdminNav"] = "widgets"
	c.HTML(http.StatusOK, "admin_widgets.gohtml", data)
}

// SaveWidgets 保存组件配置（v6 格式）。
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
			if err := h.st.DeleteSetting(c, "widget_"+areaKey); err != nil {
				h.serverError(c, err)
				return
			}
			continue
		}

		// 构建 v6 格式: [{"id":"x","opts":{...}}]
		var items []theme.WidgetConfigItem
		for i, id := range ids {
			item := theme.WidgetConfigItem{ID: id, Opts: make(map[string]string)}
			// 读取该位置组件的选项
			prefix := fmt.Sprintf("opts_%s_%d_", areaKey, i)
			for key, values := range c.Request.PostForm {
				if len(key) > len(prefix) && key[:len(prefix)] == prefix {
					optID := key[len(prefix):]
					if len(values) > 0 && values[0] != "" {
						item.Opts[optID] = values[0]
					}
				}
			}
			items = append(items, item)
		}

		data, err := json.Marshal(items)
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
