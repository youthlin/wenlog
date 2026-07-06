package handler

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/i18n"
	"github.com/youthlin/wenlog/internal/middleware"
	"github.com/youthlin/wenlog/internal/model"
	"github.com/youthlin/wenlog/internal/store"
)

// ProfilePage 后台个人资料页(所有角色可访问)。
func (h *Admin) ProfilePage(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	data := h.profileData(c, u)
	applyProfileMessage(c, data)
	c.HTML(http.StatusOK, "admin_profile.gohtml", data)
}

func profileRedirectURL(message string) string {
	if strings.TrimSpace(message) == "" {
		return "/admin/profile"
	}
	v := url.Values{}
	v.Set("message", message)
	return "/admin/profile?" + v.Encode()
}

func applyProfileMessage(c *gin.Context, data gin.H) {
	if c == nil {
		return
	}
	tr := i18n.Get(c)
	switch c.Query("message") {
	case "profile-saved":
		data["Notice"] = tr.T("个人资料已保存。")
	case "email-verification-sent":
		data["Notice"] = tr.T("个人资料已保存；邮箱变更验证邮件已发送，请检查新邮箱并完成验证。")
	case "email-verified":
		data["Notice"] = tr.T("邮箱已验证并更新。")
	case "password-saved":
		data["Notice"] = tr.T("密码已修改。")
	case "two-factor-enabled":
		data["Notice"] = tr.T("两步验证已开启。")
	case "two-factor-disabled":
		data["Notice"] = tr.T("两步验证已关闭。")
	}
}

// SaveProfileSettings 保存当前用户个人资料。非管理员不能自助修改用户名。
func (h *Admin) SaveProfileSettings(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	displayName := strings.TrimSpace(c.PostForm("display_name"))
	email := strings.TrimSpace(c.PostForm("email"))
	website := strings.TrimSpace(c.PostForm("website"))
	if !canEditOwnUsername(u) {
		username = u.Username
	}
	if username == "" || displayName == "" || email == "" {
		h.profileError(c, u, tr)
		return
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		h.profileError(c, u, tr)
		return
	}
	email = strings.TrimSpace(addr.Address)
	if !validOptionalURL(website) {
		data := h.profileData(c, u)
		data["Error"] = tr.T("个人网址格式不正确。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	if canEditOwnUsername(u) {
		exists, err := h.st.UserExistsByUsername(c, username, u.ID)
		if err != nil {
			h.serverError(c, err)
			return
		}
		if exists {
			data := h.profileData(c, u)
			data["Error"] = tr.T("用户名已被占用。")
			c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
			return
		}
	}
	if !sameEmail(email, u.Email) {
		h.handleEmailChange(c, tr, u, username, displayName, email, website)
		return
	}
	if err := h.st.UpdateUserProfile(c, u.ID, username, displayName, email, website); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, profileRedirectURL("profile-saved"))
}

func (h *Admin) profileError(c *gin.Context, u *model.User, tr *gettext.Translations) {
	data := h.profileData(c, u)
	if canEditOwnUsername(u) {
		data["Error"] = tr.T("用户名、显示名和邮件不能为空。")
	} else {
		data["Error"] = tr.T("显示名和邮件不能为空。")
	}
	c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
}

func (h *Admin) handleEmailChange(c *gin.Context, tr *gettext.Translations, u *model.User, username, displayName, email, website string) {
	smtpCfg := smtpConfigFromStore(c, h.st)
	if !smtpCfg.Configured() {
		data := h.profileData(c, u)
		data["Error"] = tr.T("修改邮箱需要先配置 SMTP 邮件设置，以便发送验证邮件。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	exists, err := h.st.UserExistsByEmail(c, email, u.ID)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		data := h.profileData(c, u)
		data["Error"] = tr.T("邮箱已被注册。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	token, err := randomHexToken(consts.TokenLengthVerification)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.SavePendingEmailChange(c, u.ID, email, token, time.Now().Add(consts.VerificationTokenTTL)); err != nil {
		h.serverError(c, err)
		return
	}
	siteURL, ok := configuredSiteURL(c, h.st)
	if !ok {
		data := h.profileData(c, u)
		data["Error"] = tr.T("站点 URL 未配置，无法发送安全验证链接，请联系管理员。")
		c.HTML(http.StatusInternalServerError, "admin_profile.gohtml", data)
		return
	}
	verifyURL := strings.TrimRight(siteURL, "/") + "/admin/profile/email/verify?token=" + url.QueryEscape(token)
	subject := tr.T("[%s] 邮箱变更验证", siteNameFromStore(c, h.st))
	body := tr.T("您好 %s，\n\n请点击以下链接验证并更新你的邮箱（24 小时内有效）：\n\n%s\n\n如果你没有请求修改邮箱，请忽略此邮件。\n", displayName, verifyURL)
	body = mailBodyWithSiteDomain(tr, body, siteURL)
	if err := smtpCfg.Send(email, subject, body); err != nil {
		if h.log != nil {
			h.log.ErrorContext(c, "send profile email verification", "error", err, "to", email, "user_id", u.ID)
		}
		data := h.profileData(c, u)
		data["Error"] = tr.T("邮箱验证邮件发送失败，请稍后重试或联系管理员。")
		c.HTML(http.StatusInternalServerError, "admin_profile.gohtml", data)
		return
	}
	if err := h.st.UpdateUserProfile(c, u.ID, username, displayName, u.Email, website); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, profileRedirectURL("email-verification-sent"))
}

func validOptionalURL(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// VerifyProfileEmail 完成个人资料邮箱变更验证。
func (h *Admin) VerifyProfileEmail(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		data := h.profileData(c, u)
		data["Error"] = tr.T("邮箱验证链接无效或已过期，请重新提交邮箱变更。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	updated, err := h.st.CompletePendingEmailChange(c, u.ID, token)
	if err != nil {
		data := h.profileData(c, u)
		data["Error"] = tr.T("邮箱验证链接无效或已过期，请重新提交邮箱变更。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	data := h.profileData(c, updated)
	middleware.ClearSession(c)
	data["Notice"] = tr.T("邮箱已验证并更新，请重新登录。")
	c.HTML(http.StatusOK, "admin_profile.gohtml", data)
}

// SavePasswordSettings 修改当前用户密码。
func (h *Admin) SavePasswordSettings(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	password := c.PostForm("new_password")
	confirm := c.PostForm("confirm_password")
	currentPassword := c.PostForm("current_password")
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(currentPassword)) != nil {
		data := h.profileData(c, u)
		data["Error"] = tr.T("原密码不正确。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	if password == "" || password != confirm {
		data := h.profileData(c, u)
		data["Error"] = tr.T("两次输入的密码不一致。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	if len(password) < consts.PasswordMinLen {
		data := h.profileData(c, u)
		data["Error"] = tr.T("密码长度不能少于 8 个字符。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
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
	siteName := siteNameFromStore(c, h.st)
	subject := tr.T("[%s] 密码已变更", siteName)
	body := tr.T("您好 %s，\n\n你的账户密码刚刚被修改。如果这不是你本人操作，请立即联系站点管理员。\n", u.DisplayName)
	body = mailBodyWithSiteDomain(tr, body, siteURLFromRequest(h.st, c))
	sendPasswordChangeNotification(detachedRequestContext(c), h.st, h.log, u, subject, body)
	middleware.ClearSession(c)
	c.Redirect(http.StatusSeeOther, "/auth/login")
}

// EnableTwoFactor 开启当前用户两步验证。
func (h *Admin) EnableTwoFactor(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	if u.TwoFactorEnabled {
		c.Redirect(http.StatusSeeOther, "/admin/profile")
		return
	}
	currentPassword := c.PostForm("current_password")
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(currentPassword)) != nil {
		data := h.profileData(c, u)
		data["Error"] = tr.T("原密码不正确。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	secret := normalizeTOTPSecret(c.PostForm("secret"))
	if !validTOTPSecret(secret) {
		data := h.profileData(c, u)
		data["Error"] = tr.T("两步验证密钥无效，请刷新页面后重试。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	if !verifyTOTPCode(secret, c.PostForm("code"), time.Now()) {
		data := h.profileData(c, u)
		setupURL := totpSetupURL(siteNameFromStore(c, h.st), u.Username, secret)
		data["TwoFactorSetupSecret"] = secret
		data["TwoFactorSetupURL"] = setupURL
		if qr, err := totpQRCodeDataURI(setupURL); err == nil {
			data["TwoFactorQRCode"] = qr
		}
		data["Error"] = tr.T("两步验证码不正确。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	if err := h.st.UpdateUserTwoFactor(c, u.ID, true, secret); err != nil {
		h.serverError(c, err)
		return
	}
	h.refreshCurrentSession(c, u.ID)
	c.Redirect(http.StatusSeeOther, profileRedirectURL("two-factor-enabled"))
}

// DisableTwoFactor 关闭当前用户两步验证。
func (h *Admin) DisableTwoFactor(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	if !u.TwoFactorEnabled {
		c.Redirect(http.StatusSeeOther, "/admin/profile")
		return
	}
	currentPassword := c.PostForm("current_password")
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(currentPassword)) != nil {
		data := h.profileData(c, u)
		data["Error"] = tr.T("原密码不正确。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	if !verifyTOTPCode(u.TwoFactorSecret, c.PostForm("code"), time.Now()) {
		data := h.profileData(c, u)
		data["Error"] = tr.T("两步验证码不正确。")
		c.HTML(http.StatusBadRequest, "admin_profile.gohtml", data)
		return
	}
	if err := h.st.UpdateUserTwoFactor(c, u.ID, false, ""); err != nil {
		h.serverError(c, err)
		return
	}
	h.refreshCurrentSession(c, u.ID)
	c.Redirect(http.StatusSeeOther, profileRedirectURL("two-factor-disabled"))
}

func (h *Admin) refreshCurrentSession(c *gin.Context, userID uint) {
	u, err := h.st.GetUserByID(c, userID)
	if err != nil || u == nil {
		return
	}
	middleware.SetSessionUser(c, u.ID, u.Role, u.SessionVersion)
}

// profileData 构建个人资料页数据。
func (h *Admin) profileData(c *gin.Context, u *model.User) gin.H {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("个人资料"))
	data["CurrentUser"] = u
	data["CanEditUsername"] = canEditOwnUsername(u)
	if u != nil {
		h.profilePasskeyData(c, data, u)
	}
	if u != nil && !u.TwoFactorEnabled {
		secret, err := newTOTPSecret()
		if err == nil {
			setupURL := totpSetupURL(siteNameFromStore(c, h.st), u.Username, secret)
			data["TwoFactorSetupSecret"] = secret
			data["TwoFactorSetupURL"] = setupURL
			if qr, err := totpQRCodeDataURI(setupURL); err == nil {
				data["TwoFactorQRCode"] = qr
			} else if h.log != nil {
				h.log.ErrorContext(c, "generate two factor qr", "error", err, "user_id", u.ID)
			}
		} else if h.log != nil {
			h.log.ErrorContext(c, "generate two factor secret", "error", err, "user_id", u.ID)
		}
	}
	return data
}

func canEditOwnUsername(u *model.User) bool {
	return u != nil && u.Role == model.RoleAdmin
}

// ExportDataPage 导出数据页面(GET)。
func (h *Admin) ExportDataPage(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	data := h.base(c, tr.T("导出数据"))
	data["CurrentUser"] = u
	c.HTML(http.StatusOK, "admin_export_data.gohtml", data)
}

// ExportData 导出当前用户个人数据(JSON, POST)。
func (h *Admin) ExportData(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	data, err := h.st.ExportUserData(c, u.ID)
	if err != nil {
		h.serverError(c, err)
		return
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=personal-data.json")
	if err := json.NewEncoder(c.Writer).Encode(data); err != nil {
		h.log.ErrorContext(c, "export data", "err", err)
	}
}

// DeleteAccountPage 删除账号页面(GET)。
func (h *Admin) DeleteAccountPage(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	data := h.base(c, tr.T("删除账号"))
	data["CurrentUser"] = u
	c.HTML(http.StatusOK, "admin_delete_account.gohtml", data)
}

// DeleteAccount 删除当前用户账号(POST)。
func (h *Admin) DeleteAccount(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	confirm := c.PostForm("confirm")
	if confirm != "DELETE" {
		h.renderDeleteAccountError(c, u, http.StatusBadRequest, tr.T("请输入 DELETE 确认删除。"))
		return
	}
	if err := h.st.DeleteUser(c, u.ID); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			h.renderDeleteAccountError(c, u, http.StatusBadRequest, tr.T("不能删除唯一的管理员账号。请先创建或指定另一个管理员。"))
			return
		}
		h.serverError(c, err)
		return
	}
	middleware.ClearSession(c)
	c.Redirect(http.StatusSeeOther, "/login")
}

func (h *Admin) renderDeleteAccountError(c *gin.Context, u *model.User, status int, msg string) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("删除账号"))
	data["CurrentUser"] = u
	data["Error"] = msg
	c.HTML(status, "admin_delete_account.gohtml", data)
}
