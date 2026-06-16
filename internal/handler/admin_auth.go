package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
)

// LoginForm 显示登录页。
func (h *Admin) LoginForm(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("登录"))
	if c.Query("message") == "session-secret-updated" {
		data["Notice"] = tr.T("Session Secret 已更新，所有登录用户都需要重新登录。")
	}
	data["RegistrationOpen"] = h.isRegistrationOpen(c)
	c.HTML(http.StatusOK, "admin_login.gohtml", data)
}

// Login 处理登录。
func (h *Admin) Login(c *gin.Context) {
	tr := i18n.Get(c)
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	u, err := h.st.GetUserByUsername(c, username)
	hash := ""
	if err == nil && u != nil {
		// 防时序攻击: 无论用户是否存在都执行 bcrypt 比较。
		hash = u.PasswordHash
	}
	// 使用固定 hash 作为 fallback,确保用户不存在时也消耗相近时间。
	if hash == "" {
		hash = "$2a$10$xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || err != nil {
		data := h.base(c, tr.T("登录"))
		data["Error"] = tr.T("用户名或密码错误。")
		c.HTML(http.StatusUnauthorized, "admin_login.gohtml", data)
		return
	}
	middleware.SetSessionUser(c, u.ID, u.Role, u.SessionVersion)
	c.Redirect(http.StatusSeeOther, "/admin/")
}

// Logout 退出登录。
func (h *Admin) Logout(c *gin.Context) {
	middleware.ClearSession(c)
	c.Redirect(http.StatusSeeOther, "/admin/login")
}

// RegisterForm 后台注册页(对齐 /admin/login 样式)。
func (h *Admin) RegisterForm(c *gin.Context) {
	tr := i18n.Get(c)
	if !h.isRegistrationOpen(c) {
		c.Redirect(http.StatusSeeOther, "/admin/login")
		return
	}
	data := h.base(c, tr.T("注册"))
	data["SMTPConfigured"] = smtpConfigFromStore(c, h.st).Configured()
	c.HTML(http.StatusOK, "admin_register.gohtml", data)
}

// Register 处理注册申请,先发送邮箱验证链接,验证通过后再创建账号。
func (h *Admin) Register(c *gin.Context) {
	tr := i18n.Get(c)
	if !h.isRegistrationOpen(c) {
		c.Redirect(http.StatusSeeOther, "/admin/login")
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	email := strings.TrimSpace(c.PostForm("email"))

	data := h.base(c, tr.T("注册"))
	data["RegisterUsername"] = username
	data["RegisterEmail"] = email
	data["SMTPConfigured"] = smtpConfigFromStore(c, h.st).Configured()

	if username == "" || email == "" {
		data["Error"] = tr.T("用户名和邮箱不能为空。")
		c.HTML(http.StatusBadRequest, "admin_register.gohtml", data)
		return
	}
	if err := validateUsernameT(tr.T, username); err != nil {
		data["Error"] = err.Error()
		c.HTML(http.StatusBadRequest, "admin_register.gohtml", data)
		return
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		data["Error"] = tr.T("邮箱格式不正确。")
		c.HTML(http.StatusBadRequest, "admin_register.gohtml", data)
		return
	}
	email = strings.TrimSpace(addr.Address)
	data["RegisterEmail"] = email

	smtpCfg := smtpConfigFromStore(c, h.st)
	if !smtpCfg.Configured() {
		data["Error"] = tr.T("站点尚未配置 SMTP，暂时无法发送验证邮件。")
		c.HTML(http.StatusServiceUnavailable, "admin_register.gohtml", data)
		return
	}

	exists, err := h.st.UserExistsByUsername(c, username, 0)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		data["Error"] = tr.T("用户名已被占用。")
		c.HTML(http.StatusConflict, "admin_register.gohtml", data)
		return
	}
	exists, err = h.st.UserExistsByEmail(c, email, 0)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		data["Error"] = tr.T("邮箱已被注册。")
		c.HTML(http.StatusConflict, "admin_register.gohtml", data)
		return
	}

	token, err := randomHexToken(consts.TokenLengthVerification)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.SavePendingRegistration(c, username, email, token, time.Now().Add(consts.VerificationTokenTTL)); err != nil {
		h.serverError(c, err)
		return
	}

	siteURL, ok := configuredSiteURL(c, h.st)
	if !ok {
		data["Error"] = tr.T("站点 URL 未配置，无法发送安全验证链接，请联系管理员。")
		c.HTML(http.StatusInternalServerError, "admin_register.gohtml", data)
		return
	}
	verifyURL := strings.TrimRight(siteURL, "/") + "/admin/register/verify?token=" + url.QueryEscape(token)
	siteName, _ := data["SiteName"].(string)
	subject := tr.T("[%s] 注册邮箱验证", siteName)
	body := tr.T("您好 %s，\n\n请点击以下链接验证邮箱并设置登录密码（24 小时内有效）：\n\n%s\n\n如果您没有请求注册，请忽略此邮件。\n", username, verifyURL)
	if err := smtpCfg.Send(email, subject, body); err != nil {
		if h.log != nil {
			h.log.Error("send registration verification email", "error", err, "to", email)
		}
		data["Error"] = tr.T("验证邮件发送失败，请稍后重试或联系管理员。")
		c.HTML(http.StatusInternalServerError, "admin_register.gohtml", data)
		return
	}

	data["RegisterUsername"] = ""
	data["RegisterEmail"] = ""
	data["Notice"] = tr.T("验证邮件已发送，请检查邮箱并在 24 小时内完成注册。")
	c.HTML(http.StatusOK, "admin_register.gohtml", data)
}

// RegisterVerifyForm 展示邮箱验证后的密码设置页。
func (h *Admin) RegisterVerifyForm(c *gin.Context) {
	tr := i18n.Get(c)
	if !h.isRegistrationOpen(c) {
		c.Redirect(http.StatusSeeOther, "/admin/login")
		return
	}
	token := strings.TrimSpace(c.Query("token"))
	data := h.base(c, tr.T("完成注册"))
	data["VerifyMode"] = true
	data["VerifyToken"] = token
	if token == "" {
		data["Error"] = tr.T("注册链接无效或已过期，请重新注册。")
		c.HTML(http.StatusBadRequest, "admin_register.gohtml", data)
		return
	}
	pending, err := h.st.GetPendingRegistrationByToken(c, token)
	if err != nil {
		data["Error"] = tr.T("注册链接无效或已过期，请重新注册。")
		c.HTML(http.StatusBadRequest, "admin_register.gohtml", data)
		return
	}
	if exists, err := h.st.UserExistsByUsername(c, pending.Username, 0); err != nil {
		h.serverError(c, err)
		return
	} else if exists {
		data["Error"] = tr.T("用户名已被占用，请重新注册。")
		c.HTML(http.StatusConflict, "admin_register.gohtml", data)
		return
	}
	if exists, err := h.st.UserExistsByEmail(c, pending.Email, 0); err != nil {
		h.serverError(c, err)
		return
	} else if exists {
		data["Error"] = tr.T("邮箱已被注册，请重新注册。")
		c.HTML(http.StatusConflict, "admin_register.gohtml", data)
		return
	}
	data["RegisterUsername"] = pending.Username
	data["RegisterEmail"] = pending.Email
	c.HTML(http.StatusOK, "admin_register.gohtml", data)
}

// RegisterVerify 完成邮箱验证并创建账号。
func (h *Admin) RegisterVerify(c *gin.Context) {
	tr := i18n.Get(c)
	if !h.isRegistrationOpen(c) {
		c.Redirect(http.StatusSeeOther, "/admin/login")
		return
	}
	token := strings.TrimSpace(c.PostForm("token"))
	password := c.PostForm("password")
	confirmPassword := c.PostForm("confirm_password")
	data := h.base(c, tr.T("完成注册"))
	data["VerifyMode"] = true
	data["VerifyToken"] = token
	if token == "" || password == "" {
		data["Error"] = tr.T("注册链接无效或密码不能为空。")
		c.HTML(http.StatusBadRequest, "admin_register.gohtml", data)
		return
	}
	if len(password) < consts.PasswordMinLen {
		data["Error"] = tr.T("密码长度不能少于 8 个字符。")
		c.HTML(http.StatusBadRequest, "admin_register.gohtml", data)
		return
	}
	if password != confirmPassword {
		data["Error"] = tr.T("两次输入的密码不一致。")
		c.HTML(http.StatusBadRequest, "admin_register.gohtml", data)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		h.serverError(c, err)
		return
	}
	u, err := h.st.CompletePendingRegistration(c, token, string(hash))
	if err != nil {
		data["Error"] = tr.T("注册链接无效或已过期，请重新注册。")
		c.HTML(http.StatusBadRequest, "admin_register.gohtml", data)
		return
	}
	middleware.SetSessionUser(c, u.ID, u.Role, u.SessionVersion)
	c.Redirect(http.StatusSeeOther, "/admin/")
}

func randomHexToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// isRegistrationOpen 返回当前是否开放注册。
func (h *Admin) isRegistrationOpen(ctx context.Context) bool {
	settings, err := h.st.GetSettings(ctx, consts.SettingsRegistrationOpen)
	if err != nil && h.log != nil {
		h.log.Error("get registration open setting", "error", err)
	}
	return settings[consts.SettingsRegistrationOpen] == "true"
}
