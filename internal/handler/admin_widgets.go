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
	ID           string            // 组件 ID
	Label        string            // 中文显示名
	Decl         theme.WidgetDecl  // 主题声明（含选项定义）
	Opts         map[string]string // 当前选项值
	Index        int               // 在区域内的序号
	Source       string            // builtin / theme / plugin
	ConfigSource string            // 保存到配置里的来源，如 plugin:saying
}

// areaPanel 是模板中一个区域面板的展示数据。
type areaPanel struct {
	Area              theme.WidgetArea
	AreaKey           string
	ConfiguredWidgets []configuredWidget
	ResetLegacyConfig bool
}

// WidgetsPage 组件配置页。
func (h *Admin) WidgetsPage(c *gin.Context) {
	tr := i18n.Get(c)
	t := h.currentTheme(c)
	if t == nil {
		h.serverError(c, nil)
		return
	}

	widgetDecls := theme.WidgetDeclsWithBuiltins(t)
	if h.themeManager != nil {
		widgetDecls = h.themeManager.WidgetDecls(c.Request.Context(), t, "")
	}
	widgetDecls = translateBuiltinWidgetDecls(widgetDecls, tr)

	// 可用组件列表（用于"添加"下拉）
	type availWidget struct {
		ID           string
		Label        string
		Area         string // 默认区域
		Source       string // builtin / theme / plugin
		ConfigSource string // 保存到配置里的来源，如 plugin:saying
	}
	var availableWidgets []availWidget
	for _, w := range widgetDecls {
		label := w.Label
		if label == "" {
			label = w.ID
		}
		source := w.Source
		if source == "" {
			source = theme.WidgetSourceTheme
		}
		availableWidgets = append(availableWidgets, availWidget{
			ID:           w.ID,
			Label:        label,
			Area:         w.Area,
			Source:       source,
			ConfigSource: widgetConfigSource(w),
		})
	}

	// 按区域构建面板
	var areas []areaPanel
	for areaKey, area := range t.WidgetAreas {
		configJSON := h.getWidgetConfig(c, areaKey)
		resetLegacyConfig := false
		if theme.HasLegacyWidgetConfig(configJSON) {
			_ = h.st.DeleteSetting(c, "widget_"+areaKey)
			configJSON = ""
			resetLegacyConfig = true
		}
		items := theme.ParseWidgetConfig(configJSON)

		var configured []configuredWidget
		for i, item := range items {
			decl, ok := widgetDeclForConfig(widgetDecls, item)
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
			source := decl.Source
			if source == "" {
				source = theme.WidgetSourceTheme
			}
			configured = append(configured, configuredWidget{
				ID:           item.ID,
				Label:        label,
				Decl:         decl,
				Opts:         opts,
				Index:        i,
				Source:       source,
				ConfigSource: widgetConfigSource(decl),
			})
		}

		areas = append(areas, areaPanel{
			Area:              area,
			AreaKey:           areaKey,
			ConfiguredWidgets: configured,
			ResetLegacyConfig: resetLegacyConfig,
		})
	}

	// 构建完整组件声明 JSON（含 Options），供 JS 动态构建选项表单
	widgetDeclsJSON, _ := json.Marshal(widgetDecls)

	data := h.base(c, tr.T("组件管理"))
	data["th"] = i18n.Get(c).D(t.ThemeDomain())
	data["AvailableWidgets"] = availableWidgets
	data["Areas"] = areas
	data["WidgetDeclsJSON"] = template.JS(widgetDeclsJSON)
	data["CurrentAdminNav"] = "widgets"
	c.HTML(http.StatusOK, "admin_widgets.gohtml", data)
}

// SaveWidgets 保存组件配置（对象数组格式，支持组件选项和重复组件）。
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

		// 构建对象数组格式: [{"id":"x","opts":{...}}]
		var items []theme.WidgetConfigItem
		for i, id := range ids {
			item := theme.WidgetConfigItem{ID: id, Opts: make(map[string]string)}
			if source := c.PostForm(fmt.Sprintf("widget_source_%s_%d", areaKey, i)); source != "" {
				item.Source = source
			}
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

func widgetDeclForConfig(decls []theme.WidgetDecl, item theme.WidgetConfigItem) (theme.WidgetDecl, bool) {
	if item.Source == "" {
		return theme.WidgetDecl{}, false
	}
	for _, decl := range decls {
		if decl.ID != item.ID {
			continue
		}
		source := decl.Source
		if decl.Source == theme.WidgetSourcePlugin && decl.PluginID != "" {
			source = "plugin:" + decl.PluginID
		}
		if source == item.Source || decl.Source == item.Source {
			return decl, true
		}
	}
	return theme.WidgetDecl{}, false
}

func widgetConfigSource(decl theme.WidgetDecl) string {
	if decl.Source == theme.WidgetSourcePlugin && decl.PluginID != "" {
		return "plugin:" + decl.PluginID
	}
	return decl.Source
}

func translateBuiltinWidgetDecls(decls []theme.WidgetDecl, tr interface{ T(string, ...any) string }) []theme.WidgetDecl {
	if tr == nil {
		return decls
	}
	for i := range decls {
		if !theme.IsBuiltinWidget(decls[i].ID) {
			continue
		}
		if decls[i].Label != "" {
			decls[i].Label = tr.T(decls[i].Label)
		}
		for j := range decls[i].Options {
			if decls[i].Options[j].Label != "" {
				decls[i].Options[j].Label = tr.T(decls[i].Options[j].Label)
			}
			if decls[i].Options[j].Description != "" {
				decls[i].Options[j].Description = tr.T(decls[i].Options[j].Description)
			}
			// Default 是选项默认值，不翻译
			for k := range decls[i].Options[j].Options {
				if decls[i].Options[j].Options[k].Label != "" {
					decls[i].Options[j].Options[k].Label = tr.T(decls[i].Options[j].Options[k].Label)
				}
			}
		}
	}
	return decls
}

func (h *Admin) getWidgetConfig(c *gin.Context, area string) string {
	v, err := h.st.GetSetting(c, "widget_"+area)
	if err != nil || v == "" {
		return ""
	}
	return v
}
