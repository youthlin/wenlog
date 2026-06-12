package handler

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gomarkdown/markdown"
	mhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	gettext "github.com/youthlin/t"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/blog/internal/config"
	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/i18n"
	wpimport "github.com/youthlin/blog/internal/importer"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	renderx "github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"
	"github.com/youthlin/blog/web"
)

// Admin 是后台处理器。
type Admin struct {
	st       *store.Store
	cfg      *config.Config
	log      *slog.Logger
	renderer *renderx.Renderer
	assets   hotSwitcher
}

type hotSwitcher interface {
	Hot() bool
	SetHot(bool)
}

// NewAdmin 构造后台处理器。
func NewAdmin(st *store.Store, cfg *config.Config, log *slog.Logger, renderer *renderx.Renderer, assets hotSwitcher) *Admin {
	return &Admin{st: st, cfg: cfg, log: log, renderer: renderer, assets: assets}
}

const adminPageSize = 20
const maxImportXMLSize = 50 << 20
const defaultPublicPageSize = 10
const defaultFeedSize = 20

func (h *Admin) base(c *gin.Context, title string) gin.H {
	currentPostPermalink := syncPostPermalink(h.st)
	v, _ := h.st.GetSetting(consts.SettingsSiteName)
	siteName := firstNonEmptyAdmin(v, consts.SettingsSiteNameDefault)
	data := gin.H{
		"SiteName":             siteName,
		"Title":                title,
		"PendingCount":         h.st.PendingCommentCount(),
		"PostPermalinkPattern": currentPostPermalink,
	}
	if c != nil {
		data["CSRFToken"] = middleware.CSRFToken(c)
		data["CurrentAdminNav"] = adminNavKey(c)
	}
	return i18n.Inject(c, data)
}

func adminNavKey(c *gin.Context) string {
	if c == nil {
		return ""
	}
	path := c.FullPath()
	if path == "" && c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	switch path {
	case "/admin/":
		return "dashboard"
	case "/admin/posts":
		return adminPostNavKey(c.DefaultQuery("type", model.PostTypePost))
	case "/admin/post/new", "/admin/post/:id", "/admin/post", "/admin/preview":
		postType := c.PostForm("post_type")
		if postType == "" {
			postType = c.DefaultQuery("type", model.PostTypePost)
		}
		return adminPostNavKey(postType)
	case "/admin/comments", "/admin/comment/:id/:action", "/admin/comments/edit/:id", "/admin/comments/batch":
		return "comments"
	case "/admin/categories", "/admin/category", "/admin/category/:id/delete":
		return "categories"
	case "/admin/tags", "/admin/tag", "/admin/tag/:id/delete":
		return "tags"
	case "/admin/settings", "/admin/settings/user", "/admin/settings/developer", "/admin/settings/site", "/admin/settings/session", "/admin/settings/assets/release", "/admin/settings/assets/embed", "/admin/settings/i18n/release", "/admin/settings/i18n/embed", "/admin/settings/templates/release", "/admin/settings/templates/embed", "/admin/settings/templates/reload", "/admin/settings/profile", "/admin/settings/password":
		return "settings"
	case "/admin/debug":
		return "debug"
	case "/admin/uploads", "/admin/uploads.json", "/admin/upload", "/admin/upload/:id/delete":
		return "uploads"
	case "/admin/import", "/admin/export":
		return "import"
	default:
		return ""
	}
}

func adminPostNavKey(postType string) string {
	if postType == model.PostTypePage {
		return "pages"
	}
	return "posts"
}

func settingsSection(c *gin.Context) string {
	if c == nil {
		return "general"
	}
	path := c.FullPath()
	if path == "" && c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	switch path {
	case "/admin/settings/user", "/admin/settings/profile", "/admin/settings/password":
		return "user"
	case "/admin/settings/developer", "/admin/settings/assets/release", "/admin/settings/assets/embed", "/admin/settings/i18n/release", "/admin/settings/i18n/embed", "/admin/settings/templates/release", "/admin/settings/templates/embed", "/admin/settings/templates/reload":
		return "developer"
	default:
		return "general"
	}
}

func settingsPageURL(section string) string {
	switch normalizeSettingsSection(section) {
	case "user":
		return "/admin/settings/user"
	case "developer":
		return "/admin/settings/developer"
	default:
		return "/admin/settings"
	}
}

func normalizeSettingsSection(section string) string {
	switch strings.TrimSpace(section) {
	case "account", "user":
		return "user"
	case "resources", "developer":
		return "developer"
	default:
		return "general"
	}
}

// LoginForm 显示登录页。
func (h *Admin) LoginForm(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("登录"))
	if c.Query("message") == "session-secret-updated" {
		data["Notice"] = tr.T("Session Secret 已更新，所有登录用户都需要重新登录。")
	}
	c.HTML(http.StatusOK, "admin_login.gohtml", data)
}

// Login 处理登录 当前只有管理员一种用户 所以登录就能进入管理后台
func (h *Admin) Login(c *gin.Context) {
	tr := i18n.Get(c)
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	u, err := h.st.GetUserByUsername(username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		data := h.base(c, tr.T("登录"))
		data["Error"] = tr.T("用户名或密码错误。")
		c.HTML(http.StatusUnauthorized, "admin_login.gohtml", data)
		return
	}
	s := sessions.Default(c)
	s.Set(middleware.SessionUserKey, u.ID)
	_ = s.Save()
	c.Redirect(http.StatusSeeOther, "/admin/")
}

// Logout 退出登录。
func (h *Admin) Logout(c *gin.Context) {
	s := sessions.Default(c)
	s.Clear()
	_ = s.Save()
	c.Redirect(http.StatusSeeOther, "/admin/login")
}

// Dashboard 后台首页。
func (h *Admin) Dashboard(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("后台"))
	data["Stats"] = h.st.DashboardStats()
	data["RecentPosts"] = h.st.RecentPosts(5)
	comments := h.st.RecentComments(5)
	data["RecentComments"] = comments
	if len(comments) > 0 {
		postIDs := make([]uint, 0, len(comments))
		for _, c := range comments {
			postIDs = append(postIDs, c.PostID)
		}
		postsByID, err := h.st.AdminPostsByIDs(postIDs)
		if err == nil {
			titles := make(map[uint]string, len(postsByID))
			for id, p := range postsByID {
				titles[id] = p.Title
			}
			data["CommentPostTitles"] = titles
		}
	}
	c.HTML(http.StatusOK, "admin_dashboard.gohtml", data)
}

// ImportPage 显示 WXR 导入/导出页。
func (h *Admin) ImportPage(c *gin.Context) {
	tr := i18n.Get(c)
	data, ok := h.importPageData(c, tr.T("WXR 导入 / 导出"))
	if !ok {
		return
	}
	c.HTML(http.StatusOK, "admin_import.gohtml", data)
}

// ImportXML 处理后台上传的 WXR XML,并把文章/页面归属到指定用户。
func (h *Admin) ImportXML(c *gin.Context) {
	tr := i18n.Get(c)
	data, ok := h.importPageData(c, tr.T("WXR 导入 / 导出"))
	if !ok {
		return
	}
	var form struct {
		UserID uint `form:"user_id"`
	}
	if err := c.ShouldBind(&form); err != nil {
		data["Error"] = tr.T("表单参数有误。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	data["SelectedUserID"] = form.UserID
	if form.UserID == 0 {
		data["Error"] = tr.T("请选择导入归属用户。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	user, err := h.st.GetUserByID(form.UserID)
	if err != nil {
		data["Error"] = tr.T("所选用户不存在。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	fh, err := c.FormFile("xml_file")
	if err != nil {
		data["Error"] = tr.T("请选择要导入的 XML 文件。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	if fh.Size <= 0 {
		data["Error"] = tr.T("XML 文件不能为空。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	if fh.Size > maxImportXMLSize {
		data["Error"] = tr.T("XML 文件不能超过 50MB。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	file, err := fh.Open()
	if err != nil {
		data["Error"] = tr.T("打开上传文件失败。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	defer file.Close()
	stats, err := wpimport.ImportReader(h.st.DB(), file, wpimport.Options{
		TargetUserID:  user.ID,
		IncludeDrafts: true,
	})
	if err != nil {
		h.log.Error("import xml",
			slog.Any("error", err),
			slog.String("file", fh.Filename),
			slog.Uint64("user_id", uint64(user.ID)),
		)
		data["Error"] = tr.T("导入失败: %s", err.Error())
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	data["Success"] = tr.T("导入完成，若 XML 中存在相同 ID 的文章/页面/评论，已按 upsert 覆盖保存。")
	data["TargetUser"] = user
	data["ImportStats"] = stats
	data["ImportedFileName"] = fh.Filename
	data["SelectedUserID"] = user.ID
	c.HTML(http.StatusOK, "admin_import.gohtml", data)
}

// ExportXML 导出可被当前后台重新导入的 XML。
func (h *Admin) ExportXML(c *gin.Context) {
	tr := i18n.Get(c)
	data, ok := h.importPageData(c, tr.T("WXR 导入 / 导出"))
	if !ok {
		return
	}
	var form struct {
		Posts    []string `form:"include_posts"`
		Pages    []string `form:"include_pages"`
		Comments []string `form:"include_comments"`
		Settings []string `form:"include_settings"`
	}
	if err := c.ShouldBind(&form); err != nil {
		data["Error"] = tr.T("导出表单参数有误。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	includePosts := len(form.Posts) > 0
	includePages := len(form.Pages) > 0
	includeComments := len(form.Comments) > 0
	includeSettings := len(form.Settings) > 0
	data["ExportPosts"] = includePosts
	data["ExportPages"] = includePages
	data["ExportComments"] = includeComments
	data["ExportSettings"] = includeSettings
	if !includePosts && !includePages && !includeComments && !includeSettings {
		data["Error"] = tr.T("请至少选择一项导出内容。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	v, _ := h.st.GetSetting(consts.SettingsSiteName)
	xmlData, _, err := wpimport.ExportXML(h.st.DB(), wpimport.ExportOptions{
		Posts:     includePosts,
		Pages:     includePages,
		Comments:  includeComments,
		Settings:  includeSettings,
		SiteTitle: firstNonEmptyAdmin(v, consts.SettingsSiteNameDefault),
		SiteURL:   requestBaseURL(c),
	})
	if err != nil {
		data["Error"] = tr.T("导出失败: %s", err.Error())
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	c.Header("Content-Type", "application/xml; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+wpimport.ExportFilename()+`"`)
	c.Data(http.StatusOK, "application/xml; charset=utf-8", xmlData)
}

// SettingsPage 后台设置页:站点设置 + 个人设置。
func (h *Admin) SettingsPage(c *gin.Context) {
	data := h.settingsDataForSection(c, "general")
	c.HTML(http.StatusOK, "admin_settings.gohtml", data)
}

// UserSettingsPage 后台用户设置页。
func (h *Admin) UserSettingsPage(c *gin.Context) {
	data := h.settingsDataForSection(c, "user")
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

func (h *Admin) settingsData(c *gin.Context) gin.H {
	return h.settingsDataForSection(c, settingsSection(c))
}

func (h *Admin) settingsDataForSection(c *gin.Context, section string) gin.H {
	tr := i18n.Get(c)
	currentSection := normalizeSettingsSection(section)
	title := tr.T("常规设置")
	switch currentSection {
	case "user":
		title = tr.T("用户设置")
	case "developer":
		title = tr.T("开发设置")
	}
	data := h.base(c, title)
	data["CurrentSettingsSection"] = currentSection
	settings, _ := h.st.GetSettings(
		consts.SettingsSiteName,
		consts.SettingsSiteDesc,
		consts.SettingsPostPermalink,
		consts.SettingsCategoryPrefix,
		consts.SettingsTagPrefix,
		consts.SettingsPageSize,
		consts.SettingsFeedSize,
		consts.SettingsSayingPageID,
		consts.SettingsSessionSecret,
	)
	data["SiteNameValue"] = firstNonEmptyAdmin(settings[consts.SettingsSiteName], consts.SettingsSiteNameDefault)
	data["SiteDescriptionValue"] = settings[consts.SettingsSiteDesc]
	data["PostPermalinkValue"] = firstNonEmptyAdmin(settings[consts.SettingsPostPermalink], consts.SettingsPostPermalinkDefault)
	data["CategoryPrefixValue"] = firstNonEmptyAdmin(settings[consts.SettingsCategoryPrefix], consts.SettingsCategoryPrefixDefault)
	data["TagPrefixValue"] = firstNonEmptyAdmin(settings[consts.SettingsTagPrefix], consts.SettingsTagPrefixDefault)
	data["PageSizeValue"] = positiveIntSetting(settings[consts.SettingsPageSize], defaultPublicPageSize)
	data["FeedSizeValue"] = positiveIntSetting(settings[consts.SettingsFeedSize], defaultFeedSize)
	data["SayingPageIDValue"] = positiveIntSetting(settings[consts.SettingsSayingPageID], consts.SettingsSayingPageIDDefault)
	if strings.TrimSpace(settings[consts.SettingsSessionSecret]) != "" {
		data["SessionSecretConfigured"] = true
	}
	data["TemplateHotReload"] = h.renderer != nil && h.renderer.Hot()
	data["AssetHotReload"] = h.assets != nil && h.assets.Hot()
	data["I18nHotReload"] = i18n.Hot()
	data["SettingsGeneralURL"] = settingsPageURL("general")
	data["SettingsUserURL"] = settingsPageURL("user")
	data["SettingsDeveloperURL"] = settingsPageURL("developer")
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
	if u := h.currentUser(c); u != nil {
		data["CurrentUser"] = u
	}
	return data
}

func (h *Admin) settingsDataForTab(c *gin.Context, tab string) gin.H {
	return h.settingsDataForSection(c, tab)
}

func (h *Admin) importPageData(c *gin.Context, title string) (gin.H, bool) {
	data := h.base(c, title)
	data["SelectedUserID"] = uint(0)
	data["ExportPosts"] = true
	data["ExportPages"] = true
	data["ExportComments"] = true
	data["ExportSettings"] = true
	users, err := h.st.ListUsers()
	if err != nil {
		h.serverError(c, err)
		return nil, false
	}
	data["Users"] = users
	return data, true
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
	if err := permalink.ValidatePostPattern(postPermalink); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("固定链接结构不合法: %s", err.Error())
		data["PostPermalinkValue"] = permalink.NormalizePostPattern(postPermalink)
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if err := permalink.ValidateTaxonomyPrefix(categoryPrefix); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("分类目录前缀不合法: %s", err.Error())
		data["CategoryPrefixValue"] = permalink.NormalizeTaxonomyPrefix(categoryPrefix, consts.SettingsCategoryPrefixDefault)
		data["TagPrefixValue"] = permalink.NormalizeTaxonomyPrefix(tagPrefix, consts.SettingsTagPrefixDefault)
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if err := permalink.ValidateTaxonomyPrefix(tagPrefix); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("标签前缀不合法: %s", err.Error())
		data["CategoryPrefixValue"] = permalink.NormalizeTaxonomyPrefix(categoryPrefix, consts.SettingsCategoryPrefixDefault)
		data["TagPrefixValue"] = permalink.NormalizeTaxonomyPrefix(tagPrefix, consts.SettingsTagPrefixDefault)
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	catNorm := permalink.NormalizeTaxonomyPrefix(categoryPrefix, consts.SettingsCategoryPrefixDefault)
	tagNorm := permalink.NormalizeTaxonomyPrefix(tagPrefix, consts.SettingsTagPrefixDefault)
	if catNorm == tagNorm {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("分类目录前缀和标签前缀不能相同。")
		data["CategoryPrefixValue"] = catNorm
		data["TagPrefixValue"] = tagNorm
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if err := h.st.SetSetting("site_name", name); err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.SetSetting("site_description", desc); err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.SetSetting(consts.SettingsPostPermalink, permalink.NormalizePostPattern(postPermalink)); err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.SetSetting(consts.SettingsCategoryPrefix, catNorm); err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.SetSetting(consts.SettingsTagPrefix, tagNorm); err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.SetSetting("page_size", strconv.Itoa(pageSize)); err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.SetSetting("feed_size", strconv.Itoa(feedSize)); err != nil {
		h.serverError(c, err)
		return
	}
	if err := h.st.SetSetting(consts.SettingsSayingPageID, strconv.Itoa(sayingPageID)); err != nil {
		h.serverError(c, err)
		return
	}
	syncPostPermalink(h.st)
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("general", ""))
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
	if err := h.st.SetSetting(consts.SettingsSessionSecret, secret); err != nil {
		h.serverError(c, err)
		return
	}
	s := sessions.Default(c)
	s.Clear()
	_ = s.Save()
	c.Redirect(http.StatusSeeOther, "/admin/login?message=session-secret-updated")
}

// SaveProfileSettings 保存当前用户用户名/显示名/邮箱。
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
	if username == "" || displayName == "" || email == "" {
		data := h.settingsDataForTab(c, "account")
		data["Error"] = tr.T("用户名、显示名和邮件不能为空。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	exists, err := h.st.UserExistsByUsername(username, u.ID)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		data := h.settingsDataForTab(c, "account")
		data["Error"] = tr.T("用户名已被占用。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if err := h.st.UpdateUserProfile(u.ID, username, displayName, email); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("account", ""))
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
		data := h.settingsDataForTab(c, "account")
		data["Error"] = tr.T("原密码不正确。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if password == "" || password != confirm {
		data := h.settingsDataForTab(c, "account")
		data["Error"] = tr.T("两次输入的密码不一致。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
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
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("account", ""))
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
	if pathExists("web/templates") {
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
	if pathExists("web/assets") {
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
	if !pathExists("web/i18n") {
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

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DebugPage 是只读 SQL 调试页,以 JSON 展示结果集。
func (h *Admin) DebugPage(c *gin.Context) {
	tr := i18n.Get(c)
	if c.Request.Method != http.MethodPost {
		// pageData 中含有 i18n.Inject 注入的函数, 不能序列化为 JSON
		pageData := h.base(c, tr.T("DB Debug"))
		pageData["SQL"] = ""
		c.HTML(http.StatusOK, "admin_debug.gohtml", pageData)
		return
	}

	sqlText := strings.TrimSpace(c.PostForm("sql"))
	jsonData := gin.H{"SQL": sqlText}
	if !allowDebugSQL(sqlText) {
		jsonData["Error"] = tr.T("仅允许只读 SQL(SELECT/EXPLAIN)。")
		c.JSON(http.StatusBadRequest, jsonData)
		return
	}
	rows, err := h.st.DebugQuery(sqlText)
	if err != nil {
		h.log.ErrorContext(c, "SQL执行失败",
			slog.String("sql", sqlText),
			slog.Any("error", err),
		)
		jsonData["Error"] = err.Error()
		c.JSON(http.StatusBadRequest, jsonData)
		return
	}
	h.log.InfoContext(c, "SQL执行成功",
		slog.String("sql", sqlText),
	)
	jsonData["Result"] = rows
	c.JSON(http.StatusOK, jsonData)
}

func allowDebugSQL(sqlText string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sqlText))
	return strings.HasPrefix(upper, "SELECT") ||
		strings.HasPrefix(upper, "EXPLAIN")
}

func firstNonEmptyAdmin(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func (h *Admin) currentUser(c *gin.Context) *model.User {
	uid := h.currentUserID(c)
	if uid == 0 {
		return nil
	}
	u, err := h.st.GetUserByID(uid)
	if err != nil {
		return nil
	}
	return u
}

// UploadFile 上传图片并返回可直接插入 Markdown 的地址。
func (h *Admin) UploadFile(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "message": tr.T("未登录")})
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": tr.T("未选择文件")})
		return
	}
	if fh.Size > 10<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": tr.T("文件不能超过 10MB")})
		return
	}
	file, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": tr.T("打开上传文件失败")})
		return
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, _ := io.ReadFull(file, buf)
	buf = buf[:n]
	mimeType := http.DetectContentType(buf)
	if !allowedUploadMIME(mimeType) {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": tr.T("仅支持 png/jpg/gif/webp 图片")})
		return
	}
	if _, err = file.Seek(0, 0); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": tr.T("读取上传文件失败")})
		return
	}

	now := time.Now()
	ext := safeImageExt(fh.Filename, mimeType)
	relDir := filepath.Join("wp-content", "uploads", now.Format("2006"), now.Format("01"))
	fileName := util.GenerateRandomString(24, util.WithAlphaNumer()) + ext
	absDir := filepath.Join(h.cfg.PublicDir, relDir)
	if err = os.MkdirAll(absDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": tr.T("创建上传目录失败")})
		return
	}
	absPath := filepath.Join(absDir, fileName)
	out, err := os.Create(absPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": tr.T("保存文件失败")})
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		_ = os.Remove(absPath)
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": tr.T("写入文件失败")})
		return
	}
	_ = out.Close()

	width, height := imageSize(absPath)
	urlPath := "/" + filepath.ToSlash(filepath.Join(relDir, fileName))
	record := &model.Upload{Path: urlPath, OrigName: fh.Filename, MimeType: mimeType, Size: fh.Size, Width: width, Height: height, UploaderID: u.ID, CreatedAt: now}
	if err := h.st.SaveUpload(record); err != nil {
		_ = os.Remove(absPath)
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": tr.T("保存上传记录失败")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": record.ID, "url": urlPath, "markdown": "![](" + urlPath + ")"})
}

// UploadsPage 文件管理页。
func (h *Admin) UploadsPage(c *gin.Context) {
	tr := i18n.Get(c)
	page := atoiDefault(c.Query("page"), 1)
	uploads, total, err := h.st.ListUploads(page, adminPageSize)
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, tr.T("文件管理"))
	data["Uploads"] = uploads
	data["Total"] = total
	data["Page"] = page
	data["Pages"] = int((total + int64(adminPageSize) - 1) / int64(adminPageSize))
	c.HTML(http.StatusOK, "admin_uploads.gohtml", data)
}

// UploadsJSON 返回最近上传文件的 JSON 列表,供编辑器文件选择器使用。
func (h *Admin) UploadsJSON(c *gin.Context) {
	page := atoiDefault(c.Query("page"), 1)
	uploads, _, err := h.st.ListUploads(page, adminPageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "uploads": uploads})
}

// DeleteUpload 删除上传文件及元数据。
func (h *Admin) DeleteUpload(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	u, err := h.st.GetUpload(uint(id))
	if err != nil {
		h.notFound(c)
		return
	}
	absPath := filepath.Join(h.cfg.PublicDir, strings.TrimPrefix(filepath.FromSlash(u.Path), string(filepath.Separator)))
	_ = os.Remove(absPath)
	if err := h.st.DeleteUpload(uint(id)); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/uploads")
}

func allowedUploadMIME(m string) bool {
	switch m {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func safeImageExt(name, mimeType string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch mimeType {
	case "image/png":
		if ext != ".png" {
			return ".png"
		}
	case "image/jpeg":
		if ext != ".jpg" && ext != ".jpeg" {
			return ".jpg"
		}
	case "image/gif":
		if ext != ".gif" {
			return ".gif"
		}
	case "image/webp":
		if ext != ".webp" {
			return ".webp"
		}
	}
	if ext == "" {
		return ".bin"
	}
	return ext
}

func imageSize(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// --- 文章/页面 ---

// ListPosts 后台文章/页面列表。type 查询参数区分。
func (h *Admin) ListPosts(c *gin.Context) {
	tr := i18n.Get(c)
	pt := c.DefaultQuery("type", model.PostTypePost)
	if pt != model.PostTypePost && pt != model.PostTypePage {
		pt = model.PostTypePost
	}
	keyword := strings.TrimSpace(c.Query("q"))
	categoryID := uint(atoiDefault(c.Query("category_id"), 0))
	tagID := uint(atoiDefault(c.Query("tag_id"), 0))
	if pt == model.PostTypePage {
		categoryID = 0
		tagID = 0
	}
	title := tr.T("文章管理")
	if pt == model.PostTypePage {
		title = tr.T("页面管理")
	}
	if pt == model.PostTypePost {
		if categoryID > 0 {
			for _, cat := range h.st.AllCategories() {
				if cat.ID == categoryID {
					title = tr.T("文章管理 · 分类：%s", cat.Name)
					break
				}
			}
		}
		if tagID > 0 {
			for _, tag := range h.st.AllTags() {
				if tag.ID == tagID {
					title = tr.T("文章管理 · 标签：%s", tag.Name)
					break
				}
			}
		}
	}
	if keyword != "" {
		title = tr.T("%s · 搜索：%s", title, keyword)
	}
	page := atoiDefault(c.Query("page"), 1)
	posts, total, err := h.st.AdminListPosts(pt, page, adminPageSize, categoryID, tagID, keyword)
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, title)
	data["Posts"] = posts
	data["Total"] = total
	data["PostType"] = pt
	data["CategoriesPageURL"] = termsPageURL("category")
	data["TagsPageURL"] = termsPageURL("tag")
	data["CategoryFilterID"] = categoryID
	data["TagFilterID"] = tagID
	data["Keyword"] = keyword
	data["AllCategories"] = h.st.AllCategories()
	data["AllTags"] = h.st.AllTags()
	data["ClearPostFilterURL"] = adminPostsListURL(pt, 1, 0, 0, "")
	data["Page"] = page
	pages := int((total + int64(adminPageSize) - 1) / int64(adminPageSize))
	data["Pages"] = pages
	if page > 1 {
		data["PrevPageURL"] = adminPostsListURL(pt, page-1, categoryID, tagID, keyword)
	}
	if page < pages {
		data["NextPageURL"] = adminPostsListURL(pt, page+1, categoryID, tagID, keyword)
	}
	c.HTML(http.StatusOK, "admin_posts.gohtml", data)
}

func adminPostsListURL(postType string, page int, categoryID, tagID uint, keyword string) string {
	v := url.Values{}
	v.Set("type", postType)
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if categoryID > 0 {
		v.Set("category_id", strconv.Itoa(int(categoryID)))
	}
	if tagID > 0 {
		v.Set("tag_id", strconv.Itoa(int(tagID)))
	}
	if strings.TrimSpace(keyword) != "" {
		v.Set("q", strings.TrimSpace(keyword))
	}
	return "/admin/posts?" + v.Encode()
}

func normalizeTermsSection(section string) string {
	switch strings.TrimSpace(section) {
	case "tag", "tags":
		return "tag"
	default:
		return "category"
	}
}

func termsPageURL(section string) string {
	switch normalizeTermsSection(section) {
	case "tag":
		return "/admin/tags"
	default:
		return "/admin/categories"
	}
}

func termsRedirectURL(section, message string) string {
	base := termsPageURL(section)
	v := url.Values{}
	if strings.TrimSpace(message) != "" {
		v.Set("message", message)
	}
	if encoded := v.Encode(); encoded != "" {
		return base + "?" + encoded
	}
	return base
}

func termsListURL(section, keyword string, page int) string {
	base := termsPageURL(section)
	v := url.Values{}
	if strings.TrimSpace(keyword) != "" {
		v.Set("q", strings.TrimSpace(keyword))
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if encoded := v.Encode(); encoded != "" {
		return base + "?" + encoded
	}
	return base
}

// TermsPage 兼容旧分类/标签入口,按查询参数跳转到对应的新页面。
func (h *Admin) TermsPage(c *gin.Context) {
	section := "category"
	message := c.Query("message")
	if c.Query("edit_tag") != "" || strings.HasPrefix(message, "tag-") {
		section = "tag"
	}
	c.Redirect(http.StatusSeeOther, termsPageURL(section)+"?"+c.Request.URL.RawQuery)
}

// CategoriesPage 分类管理页。
func (h *Admin) CategoriesPage(c *gin.Context) {
	data := h.termsDataForSection(c, "category")
	c.HTML(http.StatusOK, "admin_terms.gohtml", data)
}

// TagsPage 标签管理页。
func (h *Admin) TagsPage(c *gin.Context) {
	data := h.termsDataForSection(c, "tag")
	c.HTML(http.StatusOK, "admin_terms.gohtml", data)
}

func (h *Admin) termsDataForSection(c *gin.Context, section string) gin.H {
	tr := i18n.Get(c)
	currentSection := normalizeTermsSection(section)
	page := atoiDefault(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	keyword := strings.TrimSpace(c.Query("q"))
	title := tr.T("分类管理")
	if currentSection == "tag" {
		title = tr.T("标签管理")
	}
	data := h.base(c, title)
	cats := h.st.AllCategories()
	data["CurrentTermsSection"] = currentSection
	data["CurrentTermsPageURL"] = termsPageURL(currentSection)
	data["CategoriesPageURL"] = termsPageURL("category")
	data["TagsPageURL"] = termsPageURL("tag")
	data["CategoryParents"] = categoryParentNames(cats)
	data["CategoryParentOptions"] = cats
	data["CategoryForm"] = model.Category{}
	data["CategoryPostListURLs"] = categoryPostListURLs(cats)
	data["CategoryPublicURLs"] = categoryPublicURLs(cats)
	data["Keyword"] = keyword
	data["Page"] = page
	data["ListPageURL"] = termsListURL(currentSection, keyword, 1)
	if currentSection == "tag" {
		allTags := h.st.AllTags()
		tags, total, err := h.st.AdminListTags(keyword, page, adminPageSize)
		pages := int((total + int64(adminPageSize) - 1) / int64(adminPageSize))
		if err != nil {
			data["Error"] = tr.T("加载标签列表失败。")
			data["Tags"] = []model.Tag{}
			data["Total"] = int64(0)
			data["Pages"] = 0
		} else {
			data["Tags"] = tags
			data["Total"] = total
			data["Pages"] = pages
			if page > 1 {
				data["PrevPageURL"] = termsListURL(currentSection, keyword, page-1)
			}
			if page < pages {
				data["NextPageURL"] = termsListURL(currentSection, keyword, page+1)
			}
		}
		data["TagForm"] = model.Tag{}
		data["TagPostListURLs"] = tagPostListURLs(allTags)
		data["TagPublicURLs"] = tagPublicURLs(allTags)
		if c.Query("message") == "tag-saved" {
			data["Notice"] = tr.T("标签已保存。")
		}
		if c.Query("message") == "tag-deleted" {
			data["Notice"] = tr.T("标签已删除。")
		}
		if editID := atoiDefault(c.Query("edit_tag"), 0); editID > 0 {
			for _, tag := range allTags {
				if int(tag.ID) == editID {
					data["TagForm"] = tag
					data["EditingTag"] = true
					break
				}
			}
		}
		return data
	}
	categories, total, err := h.st.AdminListCategories(keyword, page, adminPageSize)
	pages := int((total + int64(adminPageSize) - 1) / int64(adminPageSize))
	if err != nil {
		data["Error"] = tr.T("加载分类列表失败。")
		data["Categories"] = []model.Category{}
		data["Total"] = int64(0)
		data["Pages"] = 0
	} else {
		data["Categories"] = categories
		data["Total"] = total
		data["Pages"] = pages
		if page > 1 {
			data["PrevPageURL"] = termsListURL(currentSection, keyword, page-1)
		}
		if page < pages {
			data["NextPageURL"] = termsListURL(currentSection, keyword, page+1)
		}
	}
	if c.Query("message") == "category-saved" {
		data["Notice"] = tr.T("分类已保存。")
	}
	if c.Query("message") == "category-deleted" {
		data["Notice"] = tr.T("分类已删除。")
	}
	if editID := atoiDefault(c.Query("edit_category"), 0); editID > 0 {
		for _, cat := range cats {
			if int(cat.ID) == editID {
				data["CategoryForm"] = cat
				data["EditingCategory"] = true
				break
			}
		}
	}
	return data
}

func categoryPostListURLs(categories []model.Category) map[uint]string {
	byID := make(map[uint]string, len(categories))
	for _, category := range categories {
		byID[category.ID] = adminPostsListURL(model.PostTypePost, 1, category.ID, 0, "")
	}
	return byID
}

func tagPostListURLs(tags []model.Tag) map[uint]string {
	byID := make(map[uint]string, len(tags))
	for _, tag := range tags {
		byID[tag.ID] = adminPostsListURL(model.PostTypePost, 1, 0, tag.ID, "")
	}
	return byID
}

func categoryPublicURLs(categories []model.Category) map[uint]string {
	byID := make(map[uint]string, len(categories))
	for _, category := range categories {
		byID[category.ID] = permalink.Category(category.Slug)
	}
	return byID
}

func tagPublicURLs(tags []model.Tag) map[uint]string {
	byID := make(map[uint]string, len(tags))
	for _, tag := range tags {
		byID[tag.ID] = permalink.Tag(tag.Slug)
	}
	return byID
}

type categoryForm struct {
	ID          uint   `form:"id"`
	Name        string `form:"name"`
	Slug        string `form:"slug"`
	Description string `form:"description"`
	ParentID    uint   `form:"parent_id"`
}

// SaveCategory 保存分类。
func (h *Admin) SaveCategory(c *gin.Context) {
	tr := i18n.Get(c)
	var f categoryForm
	if err := c.ShouldBind(&f); err != nil {
		h.serverError(c, err)
		return
	}
	name := strings.TrimSpace(f.Name)
	if name == "" {
		h.termsFormError(c, "category", tr.T("分类名称不能为空。"), model.Category{ID: f.ID, Name: f.Name, Slug: f.Slug, Description: f.Description, ParentID: f.ParentID}, model.Tag{}, true, false)
		return
	}
	slug := normalizeTermSlug(f.Slug)
	if slug == "" {
		slug = normalizeTermSlug(name)
	}
	if slug == "" {
		h.termsFormError(c, "category", tr.T("分类 slug 不能为空。"), model.Category{ID: f.ID, Name: name, Slug: f.Slug, Description: f.Description, ParentID: f.ParentID}, model.Tag{}, true, false)
		return
	}
	exists, err := h.st.CategorySlugExists(slug, f.ID)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		h.termsFormError(c, "category", tr.T("分类 slug %q 已存在。", slug), model.Category{ID: f.ID, Name: name, Slug: slug, Description: f.Description, ParentID: f.ParentID}, model.Tag{}, true, false)
		return
	}
	cat := &model.Category{ID: f.ID, Name: name, Slug: slug, Description: strings.TrimSpace(f.Description), ParentID: f.ParentID}
	if f.ID > 0 && f.ParentID == f.ID {
		cat.ParentID = 0
	}
	if err := h.st.SaveCategory(cat); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, termsRedirectURL("category", "category-saved"))
}

// DeleteCategory 删除分类。
func (h *Admin) DeleteCategory(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.st.DeleteCategory(uint(id)); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, termsRedirectURL("category", "category-deleted"))
}

type tagForm struct {
	ID   uint   `form:"id"`
	Name string `form:"name"`
	Slug string `form:"slug"`
}

// SaveTag 保存标签。
func (h *Admin) SaveTag(c *gin.Context) {
	tr := i18n.Get(c)
	var f tagForm
	if err := c.ShouldBind(&f); err != nil {
		h.serverError(c, err)
		return
	}
	name := strings.TrimSpace(f.Name)
	if name == "" {
		h.termsFormError(c, "tag", tr.T("标签名称不能为空。"), model.Category{}, model.Tag{ID: f.ID, Name: f.Name, Slug: f.Slug}, false, true)
		return
	}
	slug := normalizeTermSlug(f.Slug)
	if slug == "" {
		slug = normalizeTermSlug(name)
	}
	if slug == "" {
		h.termsFormError(c, "tag", tr.T("标签 slug 不能为空。"), model.Category{}, model.Tag{ID: f.ID, Name: name, Slug: f.Slug}, false, true)
		return
	}
	exists, err := h.st.TagSlugExists(slug, f.ID)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		h.termsFormError(c, "tag", tr.T("标签 slug %q 已存在。", slug), model.Category{}, model.Tag{ID: f.ID, Name: name, Slug: slug}, false, true)
		return
	}
	if err := h.st.SaveTag(&model.Tag{ID: f.ID, Name: name, Slug: slug}); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, termsRedirectURL("tag", "tag-saved"))
}

// DeleteTag 删除标签。
func (h *Admin) DeleteTag(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.st.DeleteTag(uint(id)); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, termsRedirectURL("tag", "tag-deleted"))
}

// EditPostForm 显示新建/编辑表单。id=0 或缺省为新建。
func (h *Admin) EditPostForm(c *gin.Context) {
	tr := i18n.Get(c)
	pt := c.DefaultQuery("type", model.PostTypePost)
	data := h.base(c, tr.T("内容管理"))
	data["PostType"] = pt
	data["AllCategories"] = h.st.AllCategories()
	data["SelectedCats"] = map[uint]bool{}
	data["TagsCSV"] = ""
	if idStr := c.Param("id"); idStr != "" && idStr != "new" {
		id, _ := strconv.ParseUint(idStr, 10, 64)
		p, err := h.st.AdminGetPost(uint(id))
		if err != nil {
			h.notFound(c)
			return
		}
		data["Post"] = p
		data["PostType"] = p.PostType
		data["IsEdit"] = true
		// 当前选中的分类 ID 集合 + 标签名(逗号分隔),供模板回填。
		selected := map[uint]bool{}
		for _, cat := range p.Categories {
			selected[cat.ID] = true
		}
		data["SelectedCats"] = selected
		var tagNames []string
		for _, t := range p.Tags {
			tagNames = append(tagNames, t.Name)
		}
		data["TagsCSV"] = strings.Join(tagNames, ", ")
	}
	c.HTML(http.StatusOK, "admin_post_edit.gohtml", data)
}

// postForm 是文章/页面编辑表单。正文统一为 Markdown。
type postForm struct {
	ID            uint   `form:"id"`
	Title         string `form:"title"`
	Slug          string `form:"slug"`
	ContentMD     string `form:"content_md"`
	Excerpt       string `form:"excerpt"`
	PostType      string `form:"post_type"`
	Status        string `form:"status"`
	CommentStatus string `form:"comment_status"`
	MenuOrder     int    `form:"menu_order"`
	CategoryIDs   []uint `form:"category_ids"`
	Tags          string `form:"tags"`
}

// SavePost 保存文章/页面。正文以 Markdown 原文存 ContentMD,渲染后的 HTML 存 Content。
func (h *Admin) SavePost(c *gin.Context) {
	tr := i18n.Get(c)
	var f postForm
	if err := c.ShouldBind(&f); err != nil {
		h.serverError(c, err)
		return
	}
	if f.PostType != model.PostTypePage {
		f.PostType = model.PostTypePost
	}
	if f.Status != model.StatusDraft {
		f.Status = model.StatusPublished
	}

	now := time.Now()
	var p *model.Post
	if f.ID > 0 {
		existing, err := h.st.AdminGetPost(f.ID)
		if err != nil {
			h.notFound(c)
			return
		}
		p = existing
	} else {
		id, err := h.st.NextPostID()
		if err != nil {
			h.serverError(c, err)
			return
		}
		p = &model.Post{ID: id, PublishedAt: now, AuthorID: h.currentUserID(c)}
	}

	p.Title = strings.TrimSpace(f.Title)
	p.Slug = strings.TrimSpace(f.Slug)
	if f.PostType == model.PostTypePage {
		if err := validatePageSlugT(tr.T, p.Slug); err != nil {
			data := h.base(c, tr.T("内容管理"))
			data["Error"] = err.Error()
			data["PostType"] = f.PostType
			data["AllCategories"] = h.st.AllCategories()
			data["SelectedCats"] = map[uint]bool{}
			data["TagsCSV"] = f.Tags
			data["Post"] = &model.Post{ID: f.ID, Title: p.Title, Slug: p.Slug, ContentMD: f.ContentMD, Excerpt: f.Excerpt, PostType: f.PostType, Status: f.Status, CommentStatus: f.CommentStatus, MenuOrder: f.MenuOrder}
			data["IsEdit"] = f.ID > 0
			c.HTML(http.StatusBadRequest, "admin_post_edit.gohtml", data)
			return
		}
		exists, err := h.st.PageSlugExists(p.Slug, p.ID)
		if err != nil {
			h.serverError(c, err)
			return
		}
		if exists {
			data := h.base(c, tr.T("内容管理"))
			data["Error"] = tr.T("页面链接 /%s 已存在，请换一个 slug。", p.Slug)
			data["PostType"] = f.PostType
			data["AllCategories"] = h.st.AllCategories()
			data["SelectedCats"] = map[uint]bool{}
			data["TagsCSV"] = f.Tags
			data["Post"] = &model.Post{ID: f.ID, Title: p.Title, Slug: p.Slug, ContentMD: f.ContentMD, Excerpt: f.Excerpt, PostType: f.PostType, Status: f.Status, CommentStatus: f.CommentStatus, MenuOrder: f.MenuOrder}
			data["IsEdit"] = f.ID > 0
			c.HTML(http.StatusBadRequest, "admin_post_edit.gohtml", data)
			return
		}
	} else {
		p.Slug = normalizeTermSlug(p.Slug)
		if p.Slug == "" {
			p.Slug = normalizeTermSlug(p.Title)
		}
		if err := validatePostSlugT(tr.T, p.Slug); err != nil {
			data := h.base(c, tr.T("内容管理"))
			data["Error"] = err.Error()
			data["PostType"] = f.PostType
			data["AllCategories"] = h.st.AllCategories()
			data["SelectedCats"] = selectedCats(f.CategoryIDs)
			data["TagsCSV"] = f.Tags
			data["Post"] = &model.Post{ID: f.ID, Title: p.Title, Slug: p.Slug, ContentMD: f.ContentMD, Excerpt: f.Excerpt, PostType: f.PostType, Status: f.Status, CommentStatus: f.CommentStatus, MenuOrder: f.MenuOrder}
			data["IsEdit"] = f.ID > 0
			c.HTML(http.StatusBadRequest, "admin_post_edit.gohtml", data)
			return
		}
		exists, err := h.st.PostSlugExists(p.Slug, p.ID)
		if err != nil {
			h.serverError(c, err)
			return
		}
		if exists {
			data := h.base(c, tr.T("内容管理"))
			data["Error"] = tr.T("文章 slug %q 已存在，请换一个。", p.Slug)
			data["PostType"] = f.PostType
			data["AllCategories"] = h.st.AllCategories()
			data["SelectedCats"] = selectedCats(f.CategoryIDs)
			data["TagsCSV"] = f.Tags
			data["Post"] = &model.Post{ID: f.ID, Title: p.Title, Slug: p.Slug, ContentMD: f.ContentMD, Excerpt: f.Excerpt, PostType: f.PostType, Status: f.Status, CommentStatus: f.CommentStatus, MenuOrder: f.MenuOrder}
			data["IsEdit"] = f.ID > 0
			c.HTML(http.StatusBadRequest, "admin_post_edit.gohtml", data)
			return
		}
	}
	p.Excerpt = f.Excerpt
	p.PostType = f.PostType
	p.Status = f.Status
	p.CommentStatus = f.CommentStatus
	p.MenuOrder = f.MenuOrder
	p.ModifiedAt = now

	// 正文:Markdown 原文 + 渲染后的 HTML 一并保存。
	p.ContentMD = f.ContentMD
	p.Content = renderMarkdown(f.ContentMD)
	p.ContentFormat = model.FormatMarkdown

	// 仅文章关联分类/标签;页面不需要。
	if f.PostType == model.PostTypePost {
		tagNames := parseTags(f.Tags)
		if err := h.st.SavePostWithTerms(p, f.CategoryIDs, tagNames); err != nil {
			h.serverError(c, err)
			return
		}
	} else if err := h.st.SavePost(p); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/posts?type="+p.PostType)
}

// currentUserID 从 session 读取当前管理员 ID。
func (h *Admin) currentUserID(c *gin.Context) uint {
	s := sessions.Default(c)
	if v := s.Get(middleware.SessionUserKey); v != nil {
		if id, ok := v.(uint); ok {
			return id
		}
	}
	return 0
}

// parseTags 把逗号分隔的标签串拆为去空白、去重的标签名切片。
func parseTags(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' }) {
		name := strings.TrimSpace(part)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func normalizeTermSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t':
			b.WriteByte('-')
		case r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

func categoryParentNames(categories []model.Category) map[uint]string {
	byID := make(map[uint]string, len(categories))
	for _, cat := range categories {
		byID[cat.ID] = cat.Name
	}
	return byID
}

func (h *Admin) termsFormError(c *gin.Context, section, msg string, cat model.Category, tag model.Tag, editingCategory, editingTag bool) {
	data := h.termsDataForSection(c, section)
	data["Error"] = msg
	data["CategoryForm"] = cat
	data["TagForm"] = tag
	data["EditingCategory"] = editingCategory
	data["EditingTag"] = editingTag
	c.HTML(http.StatusBadRequest, "admin_terms.gohtml", data)
}

// Preview 渲染 Markdown 为 HTML 片段(后台编辑预览,Ajax)。
func (h *Admin) Preview(c *gin.Context) {
	md := c.PostForm("content_md")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, renderMarkdown(md))
}

// DeletePost 删除文章/页面。
func (h *Admin) DeletePost(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.st.DeletePost(uint(id)); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/posts")
}

// --- 评论 ---

// ListComments 后台评论列表。
func (h *Admin) ListComments(c *gin.Context) {
	tr := i18n.Get(c)
	status := c.DefaultQuery("status", model.CommentApproved)
	switch status {
	case model.CommentApproved, model.CommentPending, model.CommentSpam:
	default:
		status = model.CommentApproved
	}
	postID := uint(atoiDefault(c.Query("post_id"), 0))
	page := atoiDefault(c.Query("page"), 1)
	comments, total, err := h.st.AdminListComments(status, postID, page, adminPageSize)
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, tr.T("评论管理"))
	postIDs := make([]uint, 0, len(comments))
	for _, comment := range comments {
		postIDs = append(postIDs, comment.PostID)
	}
	postsByID, err := h.st.AdminPostsByIDs(postIDs)
	if err != nil {
		h.serverError(c, err)
		return
	}
	commentPostTitles := make(map[uint]string, len(postsByID))
	commentPostLinks := make(map[uint]string, len(postsByID))
	for id, post := range postsByID {
		commentPostTitles[id] = post.Title
		if post.PostType == model.PostTypePage {
			commentPostLinks[id] = permalink.Page(&post)
		} else {
			commentPostLinks[id] = permalink.Post(&post)
		}
	}
	data["Comments"] = comments
	data["CommentPostTitles"] = commentPostTitles
	data["CommentPostLinks"] = commentPostLinks
	data["Total"] = total
	data["FilterStatus"] = status
	data["FilterPostID"] = postID
	if postID > 0 {
		if post, ok := postsByID[postID]; ok {
			data["FilterPostTitle"] = post.Title
		}
	}
	data["Counts"] = h.st.AdminCommentCounts()
	data["Page"] = page
	data["Pages"] = int((total + int64(adminPageSize) - 1) / int64(adminPageSize))
	c.HTML(http.StatusOK, "admin_comments.gohtml", data)
}

// EditComment 修改评论内容和作者元信息。
func (h *Admin) EditComment(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	fields := map[string]any{}
	if _, ok := c.GetPostForm("content"); ok {
		content := strings.TrimSpace(c.PostForm("content"))
		if content == "" {
			c.Redirect(http.StatusSeeOther, c.GetHeader("Referer"))
			return
		}
		fields["content"] = content
	}
	if _, ok := c.GetPostForm("author"); ok {
		author := strings.TrimSpace(c.PostForm("author"))
		if author == "" {
			c.Redirect(http.StatusSeeOther, c.GetHeader("Referer"))
			return
		}
		fields["author"] = author
	}
	if _, ok := c.GetPostForm("email"); ok {
		email := strings.TrimSpace(c.PostForm("email"))
		if email == "" {
			c.Redirect(http.StatusSeeOther, c.GetHeader("Referer"))
			return
		}
		if _, err := mail.ParseAddress(email); err != nil {
			c.Redirect(http.StatusSeeOther, c.GetHeader("Referer"))
			return
		}
		fields["email"] = email
	}
	if _, ok := c.GetPostForm("url"); ok {
		urlValue := strings.TrimSpace(c.PostForm("url"))
		if urlValue != "" && !strings.Contains(urlValue, "://") {
			urlValue = "https://" + urlValue
		}
		if urlValue != "" {
			if _, err := url.ParseRequestURI(urlValue); err != nil {
				c.Redirect(http.StatusSeeOther, c.GetHeader("Referer"))
				return
			}
		}
		fields["url"] = urlValue
	}
	if len(fields) == 0 {
		c.Redirect(http.StatusSeeOther, c.GetHeader("Referer"))
		return
	}
	if err := h.st.UpdateCommentFields(uint(id), fields); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, c.GetHeader("Referer"))
}

// BatchComments 批量操作评论(approve/pending/spam/delete)。
func (h *Admin) BatchComments(c *gin.Context) {
	action := c.PostForm("action")
	idStrs := c.PostFormArray("ids")
	var ids []uint
	for _, s := range idStrs {
		if n, err := strconv.ParseUint(s, 10, 64); err == nil {
			ids = append(ids, uint(n))
		}
	}
	var err error
	switch action {
	case "approve":
		err = h.st.BatchSetCommentStatus(ids, model.CommentApproved)
	case "pending":
		err = h.st.BatchSetCommentStatus(ids, model.CommentPending)
	case "spam":
		err = h.st.BatchSetCommentStatus(ids, model.CommentSpam)
	case "delete":
		err = h.st.BatchDeleteComments(ids)
	default:
		c.Redirect(http.StatusSeeOther, c.GetHeader("Referer"))
		return
	}
	if err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, c.GetHeader("Referer"))
}

// ModerateComment 审核评论(approve/spam/delete)。
func (h *Admin) ModerateComment(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	action := c.Param("action")
	var err error
	switch action {
	case "approve":
		err = h.st.SetCommentStatus(uint(id), model.CommentApproved)
	case "pending":
		err = h.st.SetCommentStatus(uint(id), model.CommentPending)
	case "spam":
		err = h.st.SetCommentStatus(uint(id), model.CommentSpam)
	case "delete":
		err = h.st.DeleteComment(uint(id))
	default:
		h.notFound(c)
		return
	}
	if err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, c.GetHeader("Referer"))
}

// --- 辅助 ---

func (h *Admin) notFound(c *gin.Context) {
	tr := i18n.Get(c)
	c.HTML(http.StatusNotFound, "admin_error.gohtml", i18n.Inject(c, gin.H{
		"Title":   "404",
		"Message": tr.T("未找到"),
	}))
}

func (h *Admin) serverError(c *gin.Context, err error) {
	h.log.Error("admin error", slog.Any("error", err), slog.String("path", c.Request.URL.Path))
	tr := i18n.Get(c)
	c.HTML(http.StatusInternalServerError, "admin_error.gohtml", i18n.Inject(c, gin.H{
		"Title":   "500",
		"Message": tr.T("服务器错误"),
	}))
}

var pageSlugReserved = map[string]bool{
	"admin":   true,
	"feed":    true,
	"healthz": true,
	"metrics": true,
	"search":  true,
}

var pageSlugAllowedRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validatePageSlug(slug string) error {
	return validatePageSlugT(func(msgID string, args ...any) string { return fmt.Sprintf(msgID, args...) }, slug)
}

func validatePageSlugT(tr func(string, ...any) string, slug string) error {
	syncPostPermalink(nil)
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return errors.New(tr(gettext.Mark.T("页面 slug 不能为空")))
	}
	if strings.ContainsRune(slug, '/') {
		return errors.New(tr(gettext.Mark.T("页面 slug 只能是单段路径，不能包含 /")))
	}
	if strings.ContainsAny(slug, "?# \t\r\n") {
		return errors.New(tr(gettext.Mark.T("页面 slug 不能包含空白、? 或 #")))
	}
	if slug == "." || slug == ".." {
		return errors.New(tr(gettext.Mark.T("页面 slug 非法")))
	}
	if !pageSlugAllowedRe.MatchString(slug) {
		return errors.New(tr(gettext.Mark.T("页面 slug 仅支持字母、数字、点、下划线和连字符，且需以字母或数字开头")))
	}
	if _, ok := permalink.ParsePostPath("/" + slug); ok {
		return errors.New(tr(gettext.Mark.T("页面 slug 不能与文章永久链接格式冲突")))
	}
	if pageSlugReserved[strings.ToLower(slug)] {
		return errors.New(tr(gettext.Mark.T("页面 slug %q 为保留路由，请换一个"), slug))
	}
	return nil
}

func validatePostSlugT(tr func(string, ...any) string, slug string) error {
	if !permalink.CurrentPatternUsesToken("postname") {
		return nil
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return errors.New(tr(gettext.Mark.T("当前固定链接结构使用了 %postname%，文章 slug 不能为空")))
	}
	if strings.ContainsRune(slug, '/') || strings.ContainsAny(slug, "?# \t\r\n") {
		return errors.New(tr(gettext.Mark.T("文章 slug 不能包含 /、空白、? 或 #")))
	}
	if slug == "." || slug == ".." {
		return errors.New(tr(gettext.Mark.T("文章 slug 非法")))
	}
	if pageSlugReserved[strings.ToLower(slug)] {
		return errors.New(tr(gettext.Mark.T("文章 slug %q 为保留路由，请换一个"), slug))
	}
	return nil
}

func selectedCats(ids []uint) map[uint]bool {
	out := make(map[uint]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// renderMarkdown 把 Markdown 渲染为 HTML,并复用前台统一的代码块高亮逻辑。
func renderMarkdown(md string) string {
	p := parser.NewWithExtensions(parser.CommonExtensions | parser.AutoHeadingIDs)
	doc := p.Parse([]byte(md))
	renderer := mhtml.NewRenderer(mhtml.RendererOptions{Flags: mhtml.CommonFlags})
	out := string(markdown.Render(doc, renderer))
	return renderx.HighlightCodeBlocks(out)
}
