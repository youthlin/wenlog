package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/theme"
)

// ThemeOptionsPage 主题选项配置页。
func (h *Admin) ThemeOptionsPage(c *gin.Context) {
	tr := i18n.Get(c)
	t := h.currentTheme(c)
	if t == nil {
		h.serverError(c, nil)
		return
	}

	if len(t.Options) == 0 {
		data := h.base(c, tr.T("主题选项"))
		data["CurrentAdminNav"] = "theme-options"
		data["NoOptions"] = true
		c.HTML(http.StatusOK, "admin_theme_options.gohtml", data)
		return
	}

	// 读取当前配置值
	type optionData struct {
		theme.OptionDecl
		Value string // 用户当前配置的值
	}
	var opts []optionData
	for _, opt := range t.Options {
		key := theme.OptionKey(t.Name, opt.ID)
		val, _ := h.st.GetSetting(c, key)
		if val == "" {
			val = opt.Default
		}
		opts = append(opts, optionData{OptionDecl: opt, Value: val})
	}

	data := h.base(c, tr.T("主题选项"))
	data["th"] = i18n.Get(c).D(t.ThemeDomain())
	data["CurrentAdminNav"] = "theme-options"
	data["ThemeName"] = t.Name
	data["Options"] = opts
	c.HTML(http.StatusOK, "admin_theme_options.gohtml", data)
}

// SaveThemeOptions 保存主题选项。
func (h *Admin) SaveThemeOptions(c *gin.Context) {
	tr := i18n.Get(c)
	t := h.currentTheme(c)
	if t == nil {
		h.serverError(c, nil)
		return
	}

	for _, opt := range t.Options {
		val := c.PostForm("option_" + opt.ID)
		key := theme.OptionKey(t.Name, opt.ID)
		if val == "" || val == opt.Default {
			// 空值或等于默认值时删除记录（回退到默认）
			_ = h.st.DeleteSetting(c, key)
		} else {
			if err := h.st.SaveSetting(c, key, val); err != nil {
				h.serverError(c, err)
				return
			}
		}
	}

	c.Redirect(http.StatusSeeOther, "/admin/theme-options?notice="+tr.T("主题选项已保存"))
}
