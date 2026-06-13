package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
)

// RegisterForm 前台注册页。
func (h *Public) RegisterForm(c *gin.Context) {
	tr := i18n.Get(c)
	s := h.loadSettings()
	data := h.base(c, tr.T("注册"), "", s)
	c.HTML(http.StatusOK, "auth_register.gohtml", data)
}

// Register 处理前台注册。
func (h *Public) Register(c *gin.Context) {
	tr := i18n.Get(c)
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	email := strings.TrimSpace(c.PostForm("email"))
	displayName := strings.TrimSpace(c.PostForm("display_name"))

	if username == "" || password == "" || email == "" {
		s := h.loadSettings()
		data := h.base(c, tr.T("注册"), "", s)
		data["Error"] = tr.T("用户名、密码和邮箱不能为空。")
		c.HTML(http.StatusBadRequest, "auth_register.gohtml", data)
		return
	}

	exists, err := h.st.UserExistsByUsername(username, 0)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		s := h.loadSettings()
		data := h.base(c, tr.T("注册"), "", s)
		data["Error"] = tr.T("用户名已被占用。")
		c.HTML(http.StatusConflict, "auth_register.gohtml", data)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		h.serverError(c, err)
		return
	}

	if displayName == "" {
		displayName = username
	}
	if err := h.st.UpsertUserPassword(username, displayName, string(hash)); err != nil {
		h.serverError(c, err)
		return
	}

	// 注册后自动登录。
	u, err := h.st.GetUserByUsername(username)
	if err != nil {
		h.serverError(c, err)
		return
	}
	middleware.SetSessionUser(c, u.ID, u.Role)
	c.Redirect(http.StatusSeeOther, "/")
}

// LoginForm 前台登录页。
func (h *Public) LoginForm(c *gin.Context) {
	tr := i18n.Get(c)
	s := h.loadSettings()
	data := h.base(c, tr.T("登录"), "", s)
	c.HTML(http.StatusOK, "auth_login.gohtml", data)
}

// Login 处理前台登录。
func (h *Public) Login(c *gin.Context) {
	tr := i18n.Get(c)
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	u, err := h.st.GetUserByUsername(username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		s := h.loadSettings()
		data := h.base(c, tr.T("登录"), "", s)
		data["Error"] = tr.T("用户名或密码错误。")
		c.HTML(http.StatusUnauthorized, "auth_login.gohtml", data)
		return
	}
	middleware.SetSessionUser(c, u.ID, u.Role)
	redirect := c.DefaultQuery("redirect", "/")
	c.Redirect(http.StatusSeeOther, redirect)
}

// Logout 前台登出。
func (h *Public) Logout(c *gin.Context) {
	middleware.ClearSession(c)
	c.Redirect(http.StatusSeeOther, "/")
}
