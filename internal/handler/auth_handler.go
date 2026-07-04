package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
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
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"
)

// Auth 是认证处理器，独立于 Public 和 Admin。
type Auth struct {
	st  *store.Store
	log *slog.Logger
}

// NewAuth 构造认证处理器。
func NewAuth(st *store.Store) *Auth {
	return &Auth{
		st:  st,
		log: slog.Default().With("component", "auth-handler"),
	}
}
func (h *Auth) Store() *store.Store { return h.st }

// base 返回认证页面通用数据。
func (h *Auth) base(c *gin.Context, title string) gin.H {
	settings, err := h.st.GetSettings(c, consts.SettingsSiteName, consts.SettingsRegistrationOpen)
	if err != nil && h.log != nil {
		h.log.Error("get settings for auth base", "error", err)
	}
	siteName := util.FirstNonEmptyOr(consts.SettingsSiteNameDefault, settings[consts.SettingsSiteName])
	data := gin.H{
		"SiteName":         siteName,
		"Title":            title,
		"AssetVersion":     assetVersion(),
		"RegistrationOpen": settings[consts.SettingsRegistrationOpen] == "true",
		"SMTPConfigured":   smtpConfigFromStore(c, h.st).Configured(),
	}
	return i18n.Inject(c, data)
}

// LoginForm 显示登录页。
func (h *Auth) LoginForm(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("登录"))
	if c.Query("message") == "session-secret-updated" {
		data["Notice"] = tr.T("Session Secret 已更新，所有登录用户都需要重新登录。")
	}
	c.HTML(http.StatusOK, "auth_login.gohtml", data)
}

// Login 处理登录。
func (h *Auth) Login(c *gin.Context) {
	tr := i18n.Get(c)
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	u, err := h.st.GetUserByUsername(c, username)
	hash := ""
	if err == nil && u != nil {
		hash = u.PasswordHash
	}
	if hash == "" {
		hash = "$2a$10$xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || err != nil {
		data := h.base(c, tr.T("登录"))
		data["Error"] = tr.T("用户名或密码错误。")
		c.HTML(http.StatusUnauthorized, "auth_login.gohtml", data)
		return
	}
	middleware.SetSessionUser(c, u.ID, u.Role, u.SessionVersion)
	c.Redirect(http.StatusSeeOther, "/admin/")
}

// Logout 退出登录。
func (h *Auth) Logout(c *gin.Context) {
	middleware.ClearSession(c)
	c.Redirect(http.StatusSeeOther, "/auth/login")
}

// RegisterForm 注册页。
func (h *Auth) RegisterForm(c *gin.Context) {
	tr := i18n.Get(c)
	if !h.isRegistrationOpen(c) {
		c.Redirect(http.StatusSeeOther, "/auth/login")
		return
	}
	data := h.base(c, tr.T("注册"))
	c.HTML(http.StatusOK, "auth_register.gohtml", data)
}

// Register 处理注册申请，先发送邮箱验证链接，验证通过后再创建账号。
func (h *Auth) Register(c *gin.Context) {
	tr := i18n.Get(c)
	if !h.isRegistrationOpen(c) {
		c.Redirect(http.StatusSeeOther, "/auth/login")
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	emailAddr := strings.TrimSpace(c.PostForm("email"))

	data := h.base(c, tr.T("注册"))
	data["RegisterUsername"] = username
	data["RegisterEmail"] = emailAddr

	if username == "" || emailAddr == "" {
		data["Error"] = tr.T("用户名和邮箱不能为空。")
		c.HTML(http.StatusBadRequest, "auth_register.gohtml", data)
		return
	}
	if err := validateUsernameT(tr.T, username); err != nil {
		data["Error"] = err.Error()
		c.HTML(http.StatusBadRequest, "auth_register.gohtml", data)
		return
	}
	addr, err := mail.ParseAddress(emailAddr)
	if err != nil {
		data["Error"] = tr.T("邮箱格式不正确。")
		c.HTML(http.StatusBadRequest, "auth_register.gohtml", data)
		return
	}
	emailAddr = strings.TrimSpace(addr.Address)
	data["RegisterEmail"] = emailAddr

	smtpCfg := smtpConfigFromStore(c, h.st)
	if !smtpCfg.Configured() {
		data["Error"] = tr.T("站点尚未配置 SMTP，暂时无法发送验证邮件。")
		c.HTML(http.StatusServiceUnavailable, "auth_register.gohtml", data)
		return
	}

	exists, err := h.st.UserExistsByUsername(c, username, 0)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		data["Error"] = tr.T("用户名已被占用。")
		c.HTML(http.StatusConflict, "auth_register.gohtml", data)
		return
	}
	exists, err = h.st.UserExistsByEmail(c, emailAddr, 0)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		data["Error"] = tr.T("邮箱已被注册。")
		c.HTML(http.StatusConflict, "auth_register.gohtml", data)
		return
	}

	token, err := randomHexToken(consts.TokenLengthVerification)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.SavePendingRegistration(c, username, emailAddr, token, time.Now().Add(consts.VerificationTokenTTL)); err != nil {
		h.serverError(c, err)
		return
	}

	siteURL, ok := configuredSiteURL(c, h.st)
	if !ok {
		data["Error"] = tr.T("站点 URL 未配置，无法发送安全验证链接，请联系管理员。")
		c.HTML(http.StatusInternalServerError, "auth_register.gohtml", data)
		return
	}
	verifyURL := strings.TrimRight(siteURL, "/") + "/auth/register/verify?token=" + url.QueryEscape(token)
	siteName, _ := data["SiteName"].(string)
	subject := tr.T("[%s] 注册邮箱验证", siteName)
	body := tr.T("您好 %s，\n\n请点击以下链接验证邮箱并设置登录密码（24 小时内有效）：\n\n%s\n\n如果您没有请求注册，请忽略此邮件。\n", username, verifyURL)
	body = mailBodyWithSiteDomain(tr, body, siteURL)
	if err := smtpCfg.Send(emailAddr, subject, body); err != nil {
		if h.log != nil {
			h.log.Error("send registration verification email", "error", err, "to", emailAddr)
		}
		data["Error"] = tr.T("验证邮件发送失败，请稍后重试或联系管理员。")
		c.HTML(http.StatusInternalServerError, "auth_register.gohtml", data)
		return
	}

	data["RegisterUsername"] = ""
	data["RegisterEmail"] = ""
	data["Notice"] = tr.T("验证邮件已发送，请检查邮箱并在 24 小时内完成注册。")
	c.HTML(http.StatusOK, "auth_register.gohtml", data)
}

// RegisterVerifyForm 展示邮箱验证后的密码设置页。
func (h *Auth) RegisterVerifyForm(c *gin.Context) {
	tr := i18n.Get(c)
	if !h.isRegistrationOpen(c) {
		c.Redirect(http.StatusSeeOther, "/auth/login")
		return
	}
	token := strings.TrimSpace(c.Query("token"))
	data := h.base(c, tr.T("完成注册"))
	data["VerifyMode"] = true
	data["VerifyToken"] = token
	if token == "" {
		data["Error"] = tr.T("注册链接无效或已过期，请重新注册。")
		c.HTML(http.StatusBadRequest, "auth_register.gohtml", data)
		return
	}
	pending, err := h.st.GetPendingRegistrationByToken(c, token)
	if err != nil {
		data["Error"] = tr.T("注册链接无效或已过期，请重新注册。")
		c.HTML(http.StatusBadRequest, "auth_register.gohtml", data)
		return
	}
	if exists, err := h.st.UserExistsByUsername(c, pending.Username, 0); err != nil {
		h.serverError(c, err)
		return
	} else if exists {
		data["Error"] = tr.T("用户名已被占用，请重新注册。")
		c.HTML(http.StatusConflict, "auth_register.gohtml", data)
		return
	}
	if exists, err := h.st.UserExistsByEmail(c, pending.Email, 0); err != nil {
		h.serverError(c, err)
		return
	} else if exists {
		data["Error"] = tr.T("邮箱已被注册，请重新注册。")
		c.HTML(http.StatusConflict, "auth_register.gohtml", data)
		return
	}
	data["RegisterUsername"] = pending.Username
	data["RegisterEmail"] = pending.Email
	c.HTML(http.StatusOK, "auth_register.gohtml", data)
}

// RegisterVerify 完成邮箱验证并创建账号。
func (h *Auth) RegisterVerify(c *gin.Context) {
	tr := i18n.Get(c)
	if !h.isRegistrationOpen(c) {
		c.Redirect(http.StatusSeeOther, "/auth/login")
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
		c.HTML(http.StatusBadRequest, "auth_register.gohtml", data)
		return
	}
	if len(password) < consts.PasswordMinLen {
		data["Error"] = tr.T("密码长度不能少于 8 个字符。")
		c.HTML(http.StatusBadRequest, "auth_register.gohtml", data)
		return
	}
	if password != confirmPassword {
		data["Error"] = tr.T("两次输入的密码不一致。")
		c.HTML(http.StatusBadRequest, "auth_register.gohtml", data)
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
		c.HTML(http.StatusBadRequest, "auth_register.gohtml", data)
		return
	}
	middleware.SetSessionUser(c, u.ID, u.Role, u.SessionVersion)
	c.Redirect(http.StatusSeeOther, "/admin/")
}

// ForgotPasswordForm 忘记密码页。
func (h *Auth) ForgotPasswordForm(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("忘记密码"))
	c.HTML(http.StatusOK, "auth_forgot_password.gohtml", data)
}

// ForgotPassword 处理忘记密码请求，发送重置邮件。
func (h *Auth) ForgotPassword(c *gin.Context) {
	tr := i18n.Get(c)
	emailAddr := strings.TrimSpace(c.PostForm("email"))

	if emailAddr == "" {
		data := h.base(c, tr.T("忘记密码"))
		data["Error"] = tr.T("请输入邮箱地址。")
		c.HTML(http.StatusBadRequest, "auth_forgot_password.gohtml", data)
		return
	}

	u, err := h.st.GetUserByEmail(c, emailAddr)
	if err != nil {
		time.Sleep(consts.TimingAttackDelay * time.Millisecond)
		data := h.base(c, tr.T("忘记密码"))
		data["Success"] = tr.T("如果该邮箱已注册，重置密码链接已发送。")
		c.HTML(http.StatusOK, "auth_forgot_password.gohtml", data)
		return
	}

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

	smtpCfg := smtpConfigFromStore(c, h.st)
	siteURL, ok := configuredSiteURL(c, h.st)
	if !ok {
		if err := h.st.ClearResetToken(c, u.ID); err != nil && h.log != nil {
			h.log.Error("clear reset token", "error", err, "user_id", u.ID)
		}
		data := h.base(c, tr.T("忘记密码"))
		data["Error"] = tr.T("站点 URL 未配置，无法发送安全重置链接，请联系管理员。")
		c.HTML(http.StatusInternalServerError, "auth_forgot_password.gohtml", data)
		return
	}
	resetURL := strings.TrimRight(siteURL, "/") + "/auth/reset-password?token=" + token
	siteName := siteNameFromStore(c, h.st)
	body := tr.T("您好 %s，\n\n请点击以下链接重置密码（1 小时内有效）：\n\n%s\n\n如果您没有请求重置密码，请忽略此邮件。\n", u.DisplayName, resetURL)
	body = mailBodyWithSiteDomain(tr, body, siteURL)
	subject := tr.T("[%s] 密码重置", siteName)

	if err := smtpCfg.Send(emailAddr, subject, body); err != nil {
		h.log.Error("send reset email", "error", err, "to", emailAddr)
		if err2 := h.st.ClearResetToken(c, u.ID); err2 != nil && h.log != nil {
			h.log.Error("clear reset token", "error", err2, "user_id", u.ID)
		}
		data := h.base(c, tr.T("忘记密码"))
		data["Error"] = tr.T("邮件发送失败，请稍后重试或联系管理员。")
		c.HTML(http.StatusInternalServerError, "auth_forgot_password.gohtml", data)
		return
	}

	data := h.base(c, tr.T("忘记密码"))
	data["Success"] = tr.T("如果该邮箱已注册，重置密码链接已发送。")
	c.HTML(http.StatusOK, "auth_forgot_password.gohtml", data)
}

// ResetPasswordForm 重置密码页（校验 token）。
func (h *Auth) ResetPasswordForm(c *gin.Context) {
	tr := i18n.Get(c)
	token := strings.TrimSpace(c.Query("token"))

	if token == "" {
		c.Redirect(http.StatusSeeOther, "/auth/forgot-password")
		return
	}

	u, err := h.st.GetUserByResetToken(c, token)
	if err != nil {
		data := h.base(c, tr.T("重置密码"))
		data["Error"] = tr.T("重置链接无效或已过期，请重新请求。")
		c.HTML(http.StatusBadRequest, "auth_reset_password.gohtml", data)
		return
	}

	data := h.base(c, tr.T("重置密码"))
	data["ResetToken"] = token
	data["ResetUser"] = u
	c.HTML(http.StatusOK, "auth_reset_password.gohtml", data)
}

// ResetPassword 处理重置密码提交。
func (h *Auth) ResetPassword(c *gin.Context) {
	tr := i18n.Get(c)
	token := strings.TrimSpace(c.PostForm("token"))
	password := c.PostForm("password")
	confirmPassword := c.PostForm("confirm_password")

	if token == "" || password == "" {
		c.Redirect(http.StatusSeeOther, "/auth/forgot-password")
		return
	}

	if len(password) < consts.PasswordMinLen {
		data := h.base(c, tr.T("重置密码"))
		data["ResetToken"] = token
		data["Error"] = tr.T("密码长度不能少于 8 个字符。")
		c.HTML(http.StatusBadRequest, "auth_reset_password.gohtml", data)
		return
	}

	if password != confirmPassword {
		data := h.base(c, tr.T("重置密码"))
		data["ResetToken"] = token
		data["Error"] = tr.T("两次输入的密码不一致。")
		c.HTML(http.StatusBadRequest, "auth_reset_password.gohtml", data)
		return
	}

	u, err := h.st.GetUserByResetToken(c, token)
	if err != nil {
		data := h.base(c, tr.T("重置密码"))
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
	if err := h.st.ClearResetToken(c, u.ID); err != nil && h.log != nil {
		h.log.Error("clear reset token", "error", err, "user_id", u.ID)
	}

	siteName := siteNameFromStore(c, h.st)
	subject := tr.T("[%s] 密码已变更", siteName)
	body := tr.T("您好 %s，\n\n你的账户密码刚刚通过重置链接被修改。如果这不是你本人操作，请立即联系站点管理员。\n", u.DisplayName)
	body = mailBodyWithSiteDomain(tr, body, siteURLFromRequest(h.st, c))
	sendPasswordChangeNotification(h.st, h.log, u, subject, body)

	data := h.base(c, tr.T("重置密码"))
	data["Success"] = tr.T("密码已重置，请使用新密码登录。")
	c.HTML(http.StatusOK, "auth_reset_password.gohtml", data)
}

// isRegistrationOpen 返回当前是否开放注册。
func (h *Auth) isRegistrationOpen(ctx context.Context) bool {
	settings, err := h.st.GetSettings(ctx, consts.SettingsRegistrationOpen)
	if err != nil && h.log != nil {
		h.log.Error("get registration open setting", "error", err)
	}
	return settings[consts.SettingsRegistrationOpen] == "true"
}

// serverError 处理服务端错误。
func (h *Auth) serverError(c *gin.Context, err error) {
	if h.log != nil {
		h.log.Error("auth server error", "error", err)
	}
	c.String(http.StatusInternalServerError, "Internal Server Error")
}

// sendPasswordChangeNotification 发送密码变更通知邮件（包级辅助函数）。
func sendPasswordChangeNotification(st *store.Store, log *slog.Logger, u *model.User, subject, body string) {
	smtpCfg := smtpConfigFromStore(context.Background(), st)
	if !smtpCfg.Configured() || u.Email == "" {
		return
	}
	go func() {
		if err := smtpCfg.Send(u.Email, subject, body); err != nil && log != nil {
			log.Error("send password change notification", "error", err, "to", u.Email, "user_id", u.ID)
		}
	}()
}
