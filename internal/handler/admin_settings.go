package handler

import (
	"context"
	"io/fs"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/util"
	"github.com/youthlin/blog/web"
)

func settingsPageURL(section string) string {
	switch normalizeSettingsSection(section) {
	case "developer":
		return "/admin/settings/developer"
	default:
		return "/admin/settings"
	}
}

func normalizeSettingsSection(section string) string {
	switch strings.TrimSpace(section) {
	case "resources", "developer":
		return "developer"
	default:
		return "general"
	}
}

// SettingsPage 后台设置页:站点设置。
func (h *Admin) SettingsPage(c *gin.Context) {
	data := h.settingsDataForSection(c, "general")
	c.HTML(http.StatusOK, "admin_settings.gohtml", data)
}

// DeveloperSettingsPage 后台开发设置页。
func (h *Admin) DeveloperSettingsPage(c *gin.Context) {
	data := h.settingsDataForSection(c, "developer")
	c.HTML(http.StatusOK, "admin_settings.gohtml", data)
}
func settingsRedirectURL(section, message string) string {
	base := settingsPageURL(normalizeSettingsSection(section))
	v := url.Values{}
	if strings.TrimSpace(message) != "" {
		v.Set("message", message)
	}
	if encoded := v.Encode(); encoded != "" {
		return base + "?" + encoded
	}
	return base
}

func (h *Admin) settingsDataForSection(c *gin.Context, section string) gin.H {
	tr := i18n.Get(c)
	currentSection := normalizeSettingsSection(section)
	title := tr.T("常规设置")
	switch currentSection {
	case "developer":
		title = tr.T("开发设置")
	}
	data := h.base(c, title)
	data["CurrentSettingsSection"] = currentSection
	settings, err := h.st.GetSettings(c, consts.SettingsSiteName,
		consts.SettingsSiteDesc,
		consts.SettingsPostPermalink,
		consts.SettingsCategoryPrefix,
		consts.SettingsTagPrefix,
		consts.SettingsPageSize,
		consts.SettingsFeedSize,
		consts.SettingsSayingPageID,
		consts.SettingsDefaultAvatar,
		consts.SettingsSessionSecret,
		consts.SettingsRegistrationOpen,
		consts.SettingsSMTPHost,
		consts.SettingsSMTPPort,
		consts.SettingsSMTPUser,
		consts.SettingsSMTPPassword,
		consts.SettingsSMTPFrom,
		consts.SettingsSiteURL,
		consts.SettingsMetricsAuthPassword,
	)
	if err != nil && h.log != nil {
		h.log.Error("get settings for settings page", "error", err)
	}
	data["SiteNameValue"] = util.FirstNonEmptyOr(consts.SettingsSiteNameDefault, settings[consts.SettingsSiteName])
	data["SiteDescriptionValue"] = settings[consts.SettingsSiteDesc]
	data["PostPermalinkValue"] = util.FirstNonEmptyOr(consts.SettingsPostPermalinkDefault, settings[consts.SettingsPostPermalink])
	data["CategoryPrefixValue"] = util.FirstNonEmptyOr(consts.SettingsCategoryPrefixDefault, settings[consts.SettingsCategoryPrefix])
	data["TagPrefixValue"] = util.FirstNonEmptyOr(consts.SettingsTagPrefixDefault, settings[consts.SettingsTagPrefix])
	data["PageSizeValue"] = positiveIntSetting(settings[consts.SettingsPageSize], defaultPublicPageSize)
	data["FeedSizeValue"] = positiveIntSetting(settings[consts.SettingsFeedSize], defaultFeedSize)
	data["SayingPageIDValue"] = positiveIntSetting(settings[consts.SettingsSayingPageID], consts.SettingsSayingPageIDDefault)
	data["DefaultAvatarValue"] = util.NormalizeDefaultAvatar(settings[consts.SettingsDefaultAvatar])
	data["RegistrationOpenValue"] = settings[consts.SettingsRegistrationOpen] == "true"
	data["SMTPHostValue"] = settings[consts.SettingsSMTPHost]
	data["SMTPPortValue"] = settings[consts.SettingsSMTPPort]
	data["SMTPUserValue"] = settings[consts.SettingsSMTPUser]
	data["SMTPPasswordValue"] = settings[consts.SettingsSMTPPassword]
	data["SMTPFromValue"] = settings[consts.SettingsSMTPFrom]
	data["SiteURLValue"] = settings[consts.SettingsSiteURL]
	data["MetricsAuthUsernameValue"] = "metrics"
	data["MetricsAuthPasswordValue"] = h.ensureMetricsAuthPassword(c, settings[consts.SettingsMetricsAuthPassword])
	data["SMTPConfigured"] = smtpConfigFromSettings(settings).Configured()
	if strings.TrimSpace(settings[consts.SettingsSessionSecret]) != "" {
		data["SessionSecretConfigured"] = true
	}
	data["TemplateHotReload"] = h.renderer != nil && h.renderer.Hot()
	data["AssetHotReload"] = h.assets != nil && h.assets.Hot()
	data["I18nHotReload"] = i18n.Hot()
	data["ShowSQLDetails"] = settings[consts.SettingsShowSQLDetails] == "true"
	data["SettingsGeneralURL"] = settingsPageURL("general")
	data["SettingsDeveloperURL"] = settingsPageURL("developer")
	// 主题管理数据
	if h.themeManager != nil {
		data["Themes"] = h.themeManager.List()
		data["CurrentTheme"] = h.themeManager.Current(c)
	}
	if c != nil && c.Query("message") == "templates-reloaded" {
		data["Notice"] = tr.T("模板已重新解析。")
	}
	if c != nil && c.Query("message") == "templates-released" {
		data["Notice"] = tr.T("已切换到本地模板 hot 模式；若 `web/templates` 已存在，则会直接复用现有文件，不覆盖你的改动。")
	}
	if c != nil && c.Query("message") == "templates-embedded" {
		data["Notice"] = tr.T("已切回使用内嵌模板资源；本地模板目录会保留在磁盘上。")
	}
	if c != nil && c.Query("message") == "assets-released" {
		data["Notice"] = tr.T("已切换到本地资源 hot 模式；若 `web/assets` 已存在，则会直接复用现有文件，不覆盖你的改动。")
	}
	if c != nil && c.Query("message") == "assets-embedded" {
		data["Notice"] = tr.T("已切回使用内嵌资源文件；本地资源目录会保留在磁盘上。")
	}
	if c != nil && c.Query("message") == "i18n-released" {
		data["Notice"] = tr.T("已切换到本地翻译资源 hot 模式；若 `web/i18n` 已存在，则会直接复用现有文件，不覆盖你的改动。")
	}
	if c != nil && c.Query("message") == "i18n-embedded" {
		data["Notice"] = tr.T("已切回使用内嵌翻译资源；本地翻译目录会保留在磁盘上。")
	}
	if c != nil && c.Query("message") == "smtp-saved" {
		data["Notice"] = tr.T("SMTP 设置已保存。")
	}
	if c != nil && c.Query("message") == "smtp-test-sent" {
		data["Notice"] = tr.T("测试邮件已发送，请检查收件箱。")
	}
	if c != nil && c.Query("message") == "metrics-saved" {
		data["Notice"] = tr.T("Metrics Basic Auth 密码已保存。")
	}
	if c != nil && c.Query("message") == "sql-details-saved" {
		data["Notice"] = tr.T("SQL 调试设置已保存。")
	}
	if c != nil && c.Query("message") == "registration-open-requires-smtp" {
		data["Error"] = tr.T("开放注册需要先配置 SMTP 邮件设置。")
	}
	if u := h.currentUser(c); u != nil {
		data["CurrentUser"] = u
	}
	return data
}

func (h *Admin) ensureMetricsAuthPassword(ctx context.Context, current string) string {
	password := strings.TrimSpace(current)
	if password != "" {
		return password
	}
	password = util.GenerateRandomString(consts.TokenLengthMetrics, util.WithAlphaNumer())
	if h != nil && h.st != nil {
		_ = h.st.SetSetting(ctx, consts.SettingsMetricsAuthPassword, password)
	}
	return password
}

func (h *Admin) settingsDataForTab(c *gin.Context, tab string) gin.H {
	return h.settingsDataForSection(c, tab)
}

// SaveSiteSettings 保存站点名称/描述。
func (h *Admin) SaveSiteSettings(c *gin.Context) {
	tr := i18n.Get(c)
	name := strings.TrimSpace(c.PostForm("site_name"))
	desc := strings.TrimSpace(c.PostForm("site_description"))
	postPermalink := strings.TrimSpace(c.PostForm("post_permalink"))
	categoryPrefix := strings.TrimSpace(c.PostForm("category_prefix"))
	tagPrefix := strings.TrimSpace(c.PostForm("tag_prefix"))
	pageSize, err := strconv.Atoi(strings.TrimSpace(c.PostForm("page_size")))
	if err != nil || pageSize < 1 {
		pageSize = defaultPublicPageSize
	}
	feedSize := positiveIntSetting(c.PostForm("feed_size"), defaultFeedSize)
	sayingPageID := positiveIntSetting(c.PostForm("saying_page_id"), consts.SettingsSayingPageIDDefault)
	defaultAvatar := util.NormalizeDefaultAvatar(c.PostForm("default_avatar"))

	catNorm, tagNorm, ok := h.validateSiteSettingsPermalink(c, tr, postPermalink, categoryPrefix, tagPrefix)
	if !ok {
		return
	}

	settings := map[string]string{
		consts.SettingsSiteName:       name,
		consts.SettingsSiteDesc:       desc,
		consts.SettingsPostPermalink:  permalink.NormalizePostPattern(postPermalink),
		consts.SettingsCategoryPrefix: catNorm,
		consts.SettingsTagPrefix:      tagNorm,
		consts.SettingsPageSize:       strconv.Itoa(pageSize),
		consts.SettingsFeedSize:       strconv.Itoa(feedSize),
		consts.SettingsSayingPageID:   strconv.Itoa(sayingPageID),
		consts.SettingsDefaultAvatar:  defaultAvatar,
	}
	if err := h.setSettings(c, settings); err != nil {
		h.serverError(c, err)
		return
	}

	registrationOpen := c.PostForm("registration_open") == "on"
	if registrationOpen && !smtpConfigFromStore(c, h.st).Configured() {
		_ = h.st.SetSetting(c, consts.SettingsRegistrationOpen, "false")
		c.Redirect(http.StatusSeeOther, settingsRedirectURL("general", "registration-open-requires-smtp"))
		return
	}
	_ = h.st.SetSetting(c, consts.SettingsRegistrationOpen, strconv.FormatBool(registrationOpen))
	syncPostPermalink(c, h.st)
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("general", ""))
}

// setSettings 批量保存设置,遇到第一个错误即返回。
func (h *Admin) setSettings(ctx context.Context, kv map[string]string) error {
	for k, v := range kv {
		if err := h.st.SetSetting(ctx, k, v); err != nil {
			return err
		}
	}
	return nil
}

func (h *Admin) validateSiteSettingsPermalink(c *gin.Context, tr *gettext.Translations, postPermalink, categoryPrefix, tagPrefix string) (catNorm, tagNorm string, ok bool) {
	if err := permalink.ValidatePostPattern(postPermalink); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("固定链接结构不合法: %s", err.Error())
		data["PostPermalinkValue"] = permalink.NormalizePostPattern(postPermalink)
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return "", "", false
	}
	if err := permalink.ValidateTaxonomyPrefix(categoryPrefix); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("分类目录前缀不合法: %s", err.Error())
		data["CategoryPrefixValue"] = permalink.NormalizeTaxonomyPrefix(categoryPrefix, consts.SettingsCategoryPrefixDefault)
		data["TagPrefixValue"] = permalink.NormalizeTaxonomyPrefix(tagPrefix, consts.SettingsTagPrefixDefault)
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return "", "", false
	}
	if err := permalink.ValidateTaxonomyPrefix(tagPrefix); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("标签前缀不合法: %s", err.Error())
		data["CategoryPrefixValue"] = permalink.NormalizeTaxonomyPrefix(categoryPrefix, consts.SettingsCategoryPrefixDefault)
		data["TagPrefixValue"] = permalink.NormalizeTaxonomyPrefix(tagPrefix, consts.SettingsTagPrefixDefault)
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return "", "", false
	}
	catNorm = permalink.NormalizeTaxonomyPrefix(categoryPrefix, consts.SettingsCategoryPrefixDefault)
	tagNorm = permalink.NormalizeTaxonomyPrefix(tagPrefix, consts.SettingsTagPrefixDefault)
	if catNorm == tagNorm {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("分类目录前缀和标签前缀不能相同。")
		data["CategoryPrefixValue"] = catNorm
		data["TagPrefixValue"] = tagNorm
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return "", "", false
	}
	return catNorm, tagNorm, true
}

// SaveSMTPSettings 只保存 SMTP 邮件设置,避免触发站点设置必填项校验。
func (h *Admin) SaveSMTPSettings(c *gin.Context) {
	if err := h.saveSMTPSettings(c); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("general", "smtp-saved"))
}

// SaveMetricsAuthSettings 保存 /metrics Basic Auth 密码。
func (h *Admin) SaveMetricsAuthSettings(c *gin.Context) {
	tr := i18n.Get(c)
	password := strings.TrimSpace(c.PostForm("metrics_auth_password"))
	if len(password) < consts.MetricsPasswordMinLen {
		data := h.settingsDataForTab(c, "developer")
		data["Error"] = tr.T("Metrics Basic Auth 密码至少需要 12 个字符。")
		data["MetricsAuthPasswordValue"] = password
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if err := h.st.SetSetting(c, consts.SettingsMetricsAuthPassword, password); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "metrics-saved"))
}

// SaveSQLDetailsSettings 保存 SQL 详情输出开关。
func (h *Admin) SaveSQLDetailsSettings(c *gin.Context) {
	showSQL := c.PostForm("show_sql_details") == "on"
	if err := h.st.SetSetting(c, consts.SettingsShowSQLDetails, strconv.FormatBool(showSQL)); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "sql-details-saved"))
}

func (h *Admin) saveSMTPSettings(c *gin.Context) error {
	smtpHost := strings.TrimSpace(c.PostForm("smtp_host"))
	smtpPort := strings.TrimSpace(c.PostForm("smtp_port"))
	smtpUser := strings.TrimSpace(c.PostForm("smtp_user"))
	smtpPassword := c.PostForm("smtp_password")
	if strings.TrimSpace(smtpPassword) == "" {
		settings, err := h.st.GetSettings(c, consts.SettingsSMTPPassword)
		if err != nil && h.log != nil {
			h.log.Error("get smtp password setting", "error", err)
		}
		smtpPassword = settings[consts.SettingsSMTPPassword]
	}
	smtpFrom := strings.TrimSpace(c.PostForm("smtp_from"))
	siteURL := strings.TrimSpace(c.PostForm("site_url"))
	settings := map[string]string{
		consts.SettingsSMTPHost:     smtpHost,
		consts.SettingsSMTPPort:     smtpPort,
		consts.SettingsSMTPUser:     smtpUser,
		consts.SettingsSMTPPassword: smtpPassword,
		consts.SettingsSMTPFrom:     smtpFrom,
		consts.SettingsSiteURL:      siteURL,
	}
	if err := h.setSettings(c, settings); err != nil {
		return err
	}
	if !smtpConfigFromSettings(settings).Configured() {
		if err := h.st.SetSetting(c, consts.SettingsRegistrationOpen, "false"); err != nil {
			return err
		}
	}
	return nil
}

// TestSMTPSettings 保存当前表单中的 SMTP 配置后发送测试邮件。
func (h *Admin) TestSMTPSettings(c *gin.Context) {
	tr := i18n.Get(c)
	if err := h.saveSMTPSettings(c); err != nil {
		h.serverError(c, err)
		return
	}
	to := strings.TrimSpace(c.PostForm("test_email"))
	if to == "" {
		if u := h.currentUser(c); u != nil {
			to = strings.TrimSpace(u.Email)
		}
	}
	if to == "" {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("测试收件人不能为空。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if _, err := mail.ParseAddress(to); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("测试收件人邮箱格式不正确。")
		data["SMTPTestEmailValue"] = to
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	smtpCfg := smtpConfigFromStore(c, h.st)
	if !smtpCfg.Configured() {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("SMTP 未配置完整，请填写服务器、端口和发件人地址。")
		data["SMTPTestEmailValue"] = to
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	siteName := siteNameFromStore(c, h.st)
	subject := tr.T("[%s] SMTP 测试邮件", siteName)
	body := tr.T("这是一封来自 %s 的 SMTP 测试邮件。\n\n如果你收到这封邮件，说明 SMTP 配置已生效。\n", siteName)
	if err := smtpCfg.Send(to, subject, body); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("测试邮件发送失败: %s", err.Error())
		data["SMTPTestEmailValue"] = to
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("general", "smtp-test-sent"))
}

// SaveSessionSettings 修改 session secret。修改后所有登录用户都需要重新登录。
func (h *Admin) SaveSessionSettings(c *gin.Context) {
	tr := i18n.Get(c)
	secret := strings.TrimSpace(c.PostForm("session_secret"))
	if secret == "" {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("Session Secret 不能为空。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if err := h.st.SetSetting(c, consts.SettingsSessionSecret, secret); err != nil {
		h.serverError(c, err)
		return
	}
	s := sessions.Default(c)
	s.Clear()
	_ = s.Save()
	c.Redirect(http.StatusSeeOther, "/admin/login?message=session-secret-updated")
}

// ReloadTemplates 手动重新解析本地模板文件。仅热更新模式支持。
func (h *Admin) ReloadTemplates(c *gin.Context) {
	tr := i18n.Get(c)
	if h.renderer == nil || !h.renderer.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前不是热更新模式，不能重新解析模板。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if err := h.renderer.Reload(); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("重新解析模板失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "templates-reloaded"))
}

// ReleaseTemplates 把嵌入模板释放到本地目录,并切换为 hot 模式。
func (h *Admin) ReleaseTemplates(c *gin.Context) {
	tr := i18n.Get(c)
	if h.renderer == nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前模板渲染器不可用。")
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if h.renderer.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前已经是 hot 模式，无需再次释放模板。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if util.PathExists("web/templates") {
		if err := h.renderer.UseFS(os.DirFS("web/templates"), true); err != nil {
			data := h.settingsDataForTab(c, "resources")
			data["Error"] = tr.T("切换到本地模板失败: %s", err.Error())
			c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
			return
		}
		c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "templates-released"))
		return
	}
	if err := h.renderer.ReleaseToHotDir("web/templates"); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("释放模板文件失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "templates-released"))
}

// UseEmbeddedTemplates 切回当前进程使用内嵌模板资源,但保留本地目录。
func (h *Admin) UseEmbeddedTemplates(c *gin.Context) {
	tr := i18n.Get(c)
	if h.renderer == nil || !h.renderer.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前不是 hot 模式，不能切回内嵌模板。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	tplFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("读取内嵌模板失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if err := h.renderer.UseFS(tplFS, false); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("切换回内嵌模板失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "templates-embedded"))
}

// ReleaseAssets 把嵌入资源释放到本地目录。释放完成后当前进程会自动优先读取该目录。
func (h *Admin) ReleaseAssets(c *gin.Context) {
	tr := i18n.Get(c)
	if h.assets == nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前资源管理器不可用。")
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if h.assets.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前已经是 hot 模式，无需再次释放资源文件。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if util.PathExists("web/assets") {
		h.assets.SetHot(true)
		c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "assets-released"))
		return
	}
	assetsFS, err := fs.Sub(web.Assets, "assets")
	if err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("读取嵌入资源失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if err := releaseDirFromFS(assetsFS, "web/assets"); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("释放资源文件失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	h.assets.SetHot(true)
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "assets-released"))
}

// UseEmbeddedAssets 切回当前进程使用内嵌资源,但保留本地目录。
func (h *Admin) UseEmbeddedAssets(c *gin.Context) {
	tr := i18n.Get(c)
	if h.assets == nil || !h.assets.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前不是 hot 模式，不能切回内嵌资源。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	h.assets.SetHot(false)
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "assets-embedded"))
}

// ReleaseI18n 把嵌入翻译资源释放到本地目录,并切换当前进程优先读取本地目录。
func (h *Admin) ReleaseI18n(c *gin.Context) {
	tr := i18n.Get(c)
	if i18n.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前已经是 hot 模式，无需再次释放翻译资源。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if !util.PathExists("web/i18n") {
		langFS, err := fs.Sub(web.I18n, "i18n")
		if err != nil {
			data := h.settingsDataForTab(c, "resources")
			data["Error"] = tr.T("读取内嵌翻译资源失败: %s", err.Error())
			c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
			return
		}
		if err := releaseDirFromFS(langFS, "web/i18n"); err != nil {
			data := h.settingsDataForTab(c, "resources")
			data["Error"] = tr.T("释放翻译资源失败: %s", err.Error())
			c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
			return
		}
	}
	i18n.SetHot(true)
	if err := i18n.Reload(); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("重新加载翻译资源失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "i18n-released"))
}

// UseEmbeddedI18n 切回当前进程使用内嵌翻译资源,但保留本地目录。
func (h *Admin) UseEmbeddedI18n(c *gin.Context) {
	tr := i18n.Get(c)
	if !i18n.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前不是 hot 模式，不能切回内嵌翻译资源。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	i18n.SetHot(false)
	if err := i18n.Reload(); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("重新加载翻译资源失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "i18n-embedded"))
}

func releaseDirFromFS(src fs.FS, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
