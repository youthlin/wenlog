package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
)

// ExportData 导出个人数据(JSON)。
func (h *Public) ExportData(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	data, err := h.st.ExportUserData(u.ID)
	if err != nil {
		h.serverError(c, err)
		return
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=personal-data.json")
	if err := json.NewEncoder(c.Writer).Encode(data); err != nil {
		h.log.Error(tr.T("导出数据失败"), "err", err)
	}
}

// DeleteAccount 注销账号。
func (h *Public) DeleteAccount(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	confirm := c.PostForm("confirm")
	if confirm != "DELETE" {
		s := h.loadSettings()
		data := h.base(c, tr.T("注销账号"), "", s)
		data["Error"] = tr.T("请输入 DELETE 确认注销。")
		c.HTML(http.StatusBadRequest, "account_delete.gohtml", data)
		return
	}
	if err := h.st.DeleteUser(u.ID); err != nil {
		h.serverError(c, err)
		return
	}
	middleware.ClearSession(c)
	c.Redirect(http.StatusSeeOther, "/")
}
