package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"context"
	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/email"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/store"
)

// ForgotPasswordForm 忘记密码页。
func (h *Public) ForgotPasswordForm(c *gin.Context) {
	tr := i18n.Get(c)
	s := h.loadSettings(c)
	data := h.base(c, tr.T("忘记密码"), "", s)
	c.HTML(http.StatusOK, "auth_forgot_password.gohtml", data)
}

// ForgotPassword 处理忘记密码请求,发送重置邮件。
func (h *Public) ForgotPassword(c *gin.Context) {
	tr := i18n.Get(c)
	s := h.loadSettings(c)
	emailAddr := strings.TrimSpace(c.PostForm("email"))

	if emailAddr == "" {
		data := h.base(c, tr.T("忘记密码"), "", s)
		data["Error"] = tr.T("请输入邮箱地址。")
		c.HTML(http.StatusBadRequest, "auth_forgot_password.gohtml", data)
		return
	}

	u, err := h.st.GetUserByEmail(c, emailAddr)
	if err != nil {
		// 防时序攻击: 用户不存在时也模拟相近的延迟。
		time.Sleep(consts.TimingAttackDelay * time.Millisecond)
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
	expiry := time.Now().Add(consts.ResetTokenTTL)

	if err := h.st.SetResetToken(c, u.ID, token, expiry); err != nil {
		h.serverError(c, err)
		return
	}

	// 发送重置邮件。安全邮件必须使用后台配置的规范站点 URL,避免 Host Header Poisoning 泄露 token。
	smtpCfg := h.loadSMTPConfig()
	siteURL, ok := h.loadConfiguredSiteURL()
	if !ok {
		if err := h.st.ClearResetToken(c, u.ID); err != nil && h.log != nil {
			h.log.Error("clear reset token", "error", err, "user_id", u.ID)
		}
		data := h.base(c, tr.T("忘记密码"), "", s)
		data["Error"] = tr.T("站点 URL 未配置，无法发送安全重置链接，请联系管理员。")
		c.HTML(http.StatusInternalServerError, "auth_forgot_password.gohtml", data)
		return
	}
	resetURL := strings.TrimRight(siteURL, "/") + "/reset-password?token=" + token
	body := tr.T("您好 %s，\n\n请点击以下链接重置密码（1 小时内有效）：\n\n%s\n\n如果您没有请求重置密码，请忽略此邮件。\n", u.DisplayName, resetURL)
	subject := tr.T("[%s] 密码重置", s.SiteName)

	if err := smtpCfg.Send(emailAddr, subject, body); err != nil {
		h.log.Error("send reset email", "error", err, "to", emailAddr)
		// 邮件发送失败,清除令牌
		if err2 := h.st.ClearResetToken(c, u.ID); err2 != nil && h.log != nil {
			h.log.Error("clear reset token", "error", err2, "user_id", u.ID)
		}
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
	s := h.loadSettings(c)
	token := strings.TrimSpace(c.Query("token"))

	if token == "" {
		c.Redirect(http.StatusSeeOther, "/forgot-password")
		return
	}

	u, err := h.st.GetUserByResetToken(c, token)
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
	s := h.loadSettings(c)
	token := strings.TrimSpace(c.PostForm("token"))
	password := c.PostForm("password")
	confirmPassword := c.PostForm("confirm_password")

	if token == "" || password == "" {
		c.Redirect(http.StatusSeeOther, "/forgot-password")
		return
	}

	if len(password) < consts.PasswordMinLen {
		data := h.base(c, tr.T("重置密码"), "", s)
		data["ResetToken"] = token
		data["Error"] = tr.T("密码长度不能少于 8 个字符。")
		c.HTML(http.StatusBadRequest, "auth_reset_password.gohtml", data)
		return
	}

	if password != confirmPassword {
		data := h.base(c, tr.T("重置密码"), "", s)
		data["ResetToken"] = token
		data["Error"] = tr.T("两次输入的密码不一致。")
		c.HTML(http.StatusBadRequest, "auth_reset_password.gohtml", data)
		return
	}

	u, err := h.st.GetUserByResetToken(c, token)
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

	if err := h.st.UpdateUserPassword(c, u.ID, string(hash)); err != nil {
		h.serverError(c, err)
		return
	}
	// 清除令牌
	if err := h.st.ClearResetToken(c, u.ID); err != nil && h.log != nil {
		h.log.Error("clear reset token", "error", err, "user_id", u.ID)
	}

	// 发送密码变更通知邮件。
	h.sendPasswordChangeNotification(u)

	data := h.base(c, tr.T("重置密码"), "", s)
	data["Success"] = tr.T("密码已重置，请使用新密码登录。")
	c.HTML(http.StatusOK, "auth_reset_password.gohtml", data)
}

func (h *Public) sendPasswordChangeNotification(u *model.User) {
	smtpCfg := h.loadSMTPConfig()
	if !smtpCfg.Configured() || u.Email == "" {
		return
	}
	subject := fmt.Sprintf("[%s] 密码已变更", siteNameFromStore(context.Background(), h.st))
	body := fmt.Sprintf("您好 %s，\n\n你的账户密码刚刚通过重置链接被修改。如果这不是你本人操作，请立即联系站点管理员。\n", u.DisplayName)
	go func() {
		if err := smtpCfg.Send(u.Email, subject, body); err != nil && h.log != nil {
			h.log.Error("send password change notification", "error", err, "to", u.Email, "user_id", u.ID)
		}
	}()
}

// loadSMTPConfig 从设置中读取 SMTP 配置。
func (h *Public) loadSMTPConfig() email.Config {
	return smtpConfigFromStore(context.Background(), h.st)
}

func smtpConfigFromStore(ctx context.Context, st *store.Store) email.Config {
	settings, _ := st.GetSettings(ctx, consts.SettingsSMTPHost,
		consts.SettingsSMTPPort,
		consts.SettingsSMTPUser,
		consts.SettingsSMTPPassword,
		consts.SettingsSMTPFrom,
	)
	return smtpConfigFromSettings(settings)
}

func smtpConfigFromSettings(settings map[string]string) email.Config {
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
	return siteURLFromRequest(h.st, c)
}

func (h *Public) loadConfiguredSiteURL() (string, bool) {
	return configuredSiteURL(context.Background(), h.st)
}

func configuredSiteURL(ctx context.Context, st *store.Store) (string, bool) {
	settings, _ := st.GetSettings(ctx, consts.SettingsSiteURL)
	u := strings.TrimSpace(settings[consts.SettingsSiteURL])
	if u == "" {
		return "", false
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	return strings.TrimRight(u, "/"), true
}

func siteURLFromRequest(st *store.Store, c *gin.Context) string {
	if u, ok := configuredSiteURL(c, st); ok {
		return u
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}
