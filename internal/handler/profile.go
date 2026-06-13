package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/blog/internal/i18n"
)

// ProfilePage 前台个人资料页。
func (h *Public) ProfilePage(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	s := h.loadSettings()
	data := h.base(c, tr.T("个人资料"), "", s)
	data["ProfileUser"] = u
	c.HTML(http.StatusOK, "profile.gohtml", data)
}

// SaveProfile 保存个人资料。
func (h *Public) SaveProfile(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	displayName := strings.TrimSpace(c.PostForm("display_name"))
	email := strings.TrimSpace(c.PostForm("email"))

	if username == "" || email == "" {
		s := h.loadSettings()
		data := h.base(c, tr.T("个人资料"), "", s)
		data["ProfileUser"] = u
		data["Error"] = tr.T("用户名和邮箱不能为空。")
		c.HTML(http.StatusBadRequest, "profile.gohtml", data)
		return
	}

	exists, err := h.st.UserExistsByUsername(username, u.ID)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		s := h.loadSettings()
		data := h.base(c, tr.T("个人资料"), "", s)
		data["ProfileUser"] = u
		data["Error"] = tr.T("用户名已被占用。")
		c.HTML(http.StatusConflict, "profile.gohtml", data)
		return
	}

	if err := h.st.UpdateUserProfile(u.ID, username, displayName, email); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/profile")
}

// ChangePassword 修改密码。
func (h *Public) ChangePassword(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	currentPassword := c.PostForm("current_password")
	newPassword := c.PostForm("new_password")

	if currentPassword == "" || newPassword == "" {
		s := h.loadSettings()
		data := h.base(c, tr.T("个人资料"), "", s)
		data["ProfileUser"] = u
		data["Error"] = tr.T("密码不能为空。")
		c.HTML(http.StatusBadRequest, "profile.gohtml", data)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(currentPassword)); err != nil {
		s := h.loadSettings()
		data := h.base(c, tr.T("个人资料"), "", s)
		data["ProfileUser"] = u
		data["Error"] = tr.T("当前密码不正确。")
		c.HTML(http.StatusBadRequest, "profile.gohtml", data)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.UpdateUserPassword(u.ID, string(hash)); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/profile")
}
