package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/email"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
)

// Logout 前台登出。
func (h *Public) Logout(c *gin.Context) {
	middleware.ClearSession(c)
	c.Redirect(http.StatusSeeOther, "/")
}

// ForgotPasswordForm 忘记密码页。
func (h *Public) ForgotPasswordForm(c *gin.Context) {
	tr := i18n.Get(c)
	s := h.loadSettings()
	data := h.base(c, tr.T("忘记密码"), "", s)
	c.HTML(http.StatusOK, "auth_forgot_password.gohtml", data)
}

// ForgotPassword 处理忘记密码请求,发送重置邮件。
func (h *Public) ForgotPassword(c *gin.Context) {
	tr := i18n.Get(c)
	s := h.loadSettings()
	emailAddr := strings.TrimSpace(c.PostForm("email"))

	if emailAddr == "" {
		data := h.base(c, tr.T("忘记密码"), "", s)
		data["Error"] = tr.T("请输入邮箱地址。")
		c.HTML(http.StatusBadRequest, "auth_forgot_password.gohtml", data)
		return
	}

	u, err := h.st.GetUserByEmail(emailAddr)
	if err != nil {
		// 不暴露用户是否存在,统一提示已发送
		data := h.base(c, tr.T("忘记密码"), "", s)
		data["Success"] = tr.T("如果该邮箱已注册，重置密码链接已发送。")
		c.HTML(http.StatusOK, "auth_forgot_password.gohtml", data)
		return
	}

	// 生成随机令牌
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		h.serverError(c, err)
		return
	}
	token := hex.EncodeToString(tokenBytes)
	expiry := time.Now().Add(1 * time.Hour)

	if err := h.st.SetResetToken(u.ID, token, expiry); err != nil {
		h.serverError(c, err)
		return
	}

	// 发送重置邮件
	smtpCfg := h.loadSMTPConfig()
	siteURL := h.loadSiteURL(c)
	resetURL := strings.TrimRight(siteURL, "/") + "/reset-password?token=" + token
	body := tr.T("您好 %s，\n\n请点击以下链接重置密码（1 小时内有效）：\n\n%s\n\n如果您没有请求重置密码，请忽略此邮件。\n", u.DisplayName, resetURL)
	subject := tr.T("[%s] 密码重置", s.SiteName)

	if err := smtpCfg.Send(emailAddr, subject, body); err != nil {
		h.log.Error("send reset email", "error", err, "to", emailAddr)
		// 邮件发送失败,清除令牌
		_ = h.st.ClearResetToken(u.ID)
		data := h.base(c, tr.T("忘记密码"), "", s)
		data["Error"] = tr.T("邮件发送失败，请稍后重试或联系管理员。")
		c.HTML(http.StatusInternalServerError, "auth_forgot_password.gohtml", data)
		return
	}

	data := h.base(c, tr.T("忘记密码"), "", s)
	data["Success"] = tr.T("如果该邮箱已注册，重置密码链接已发送。")
	c.HTML(http.StatusOK, "auth_forgot_password.gohtml", data)
}

// ResetPasswordForm 重置密码页(校验 token)。
func (h *Public) ResetPasswordForm(c *gin.Context) {
	tr := i18n.Get(c)
	s := h.loadSettings()
	token := strings.TrimSpace(c.Query("token"))

	if token == "" {
		c.Redirect(http.StatusSeeOther, "/forgot-password")
		return
	}

	u, err := h.st.GetUserByResetToken(token)
	if err != nil {
		data := h.base(c, tr.T("重置密码"), "", s)
		data["Error"] = tr.T("重置链接无效或已过期，请重新请求。")
		c.HTML(http.StatusBadRequest, "auth_reset_password.gohtml", data)
		return
	}

	data := h.base(c, tr.T("重置密码"), "", s)
	data["ResetToken"] = token
	data["ResetUser"] = u
	c.HTML(http.StatusOK, "auth_reset_password.gohtml", data)
}

// ResetPassword 处理重置密码提交。
func (h *Public) ResetPassword(c *gin.Context) {
	tr := i18n.Get(c)
	s := h.loadSettings()
	token := strings.TrimSpace(c.PostForm("token"))
	password := c.PostForm("password")
	confirmPassword := c.PostForm("confirm_password")

	if token == "" || password == "" {
		c.Redirect(http.StatusSeeOther, "/forgot-password")
		return
	}

	if password != confirmPassword {
		data := h.base(c, tr.T("重置密码"), "", s)
		data["ResetToken"] = token
		data["Error"] = tr.T("两次输入的密码不一致。")
		c.HTML(http.StatusBadRequest, "auth_reset_password.gohtml", data)
		return
	}

	u, err := h.st.GetUserByResetToken(token)
	if err != nil {
		data := h.base(c, tr.T("重置密码"), "", s)
		data["Error"] = tr.T("重置链接无效或已过期，请重新请求。")
		c.HTML(http.StatusBadRequest, "auth_reset_password.gohtml", data)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		h.serverError(c, err)
		return
	}

	if err := h.st.UpdateUserPassword(u.ID, string(hash)); err != nil {
		h.serverError(c, err)
		return
	}
	// 清除令牌
	_ = h.st.ClearResetToken(u.ID)

	data := h.base(c, tr.T("重置密码"), "", s)
	data["Success"] = tr.T("密码已重置，请使用新密码登录。")
	c.HTML(http.StatusOK, "auth_reset_password.gohtml", data)
}

// loadSMTPConfig 从设置中读取 SMTP 配置。
func (h *Public) loadSMTPConfig() email.Config {
	settings, _ := h.st.GetSettings(
		consts.SettingsSMTPHost,
		consts.SettingsSMTPPort,
		consts.SettingsSMTPUser,
		consts.SettingsSMTPPassword,
		consts.SettingsSMTPFrom,
	)
	port := 0
	if p := settings[consts.SettingsSMTPPort]; p != "" {
		port, _ = strconv.Atoi(p)
	}
	return email.Config{
		Host:     settings[consts.SettingsSMTPHost],
		Port:     port,
		User:     settings[consts.SettingsSMTPUser],
		Password: settings[consts.SettingsSMTPPassword],
		From:     settings[consts.SettingsSMTPFrom],
	}
}

// loadSiteURL 获取站点 URL,优先设置项,其次从请求推断。
func (h *Public) loadSiteURL(c *gin.Context) string {
	settings, _ := h.st.GetSettings(consts.SettingsSiteURL)
	if u := strings.TrimSpace(settings[consts.SettingsSiteURL]); u != "" {
		return u
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}
