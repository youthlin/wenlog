package handler

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"

	"github.com/youthlin/blog/internal/config"
	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/plugin"
	renderx "github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/theme"
	"github.com/youthlin/blog/internal/util"
	"github.com/youthlin/blog/internal/version"
)

// ThemeManager 是主题管理器的类型别名，方便 handler 层引用。
type ThemeManager = theme.Manager

// PluginManager 是插件管理器的类型别名，方便 handler 层引用。
type PluginManager = plugin.Manager

// Admin 是后台处理器。
type Admin struct {
	st            *store.Store
	cfg           *config.Config
	log           *slog.Logger
	renderer      *renderx.Renderer
	assets        hotSwitcher
	themeManager  *ThemeManager
	pluginManager *PluginManager
}

// NewAdmin 构造后台处理器。
func NewAdmin(st *store.Store, cfg *config.Config, renderer *renderx.Renderer, assets *LocalFirstFileSystem, tm *ThemeManager, pm *PluginManager) *Admin {
	return &Admin{
		st:            st,
		cfg:           cfg,
		log:           slog.Default().With("component", "admin-handler"),
		renderer:      renderer,
		assets:        assets,
		themeManager:  tm,
		pluginManager: pm,
	}
}

const adminPageSize = 20
const defaultPublicPageSize = 10
const defaultFeedSize = 20

// parseUintParam 解析 URL 参数为 uint,解析失败返回 0 和错误。
func parseUintParam(s string) (uint, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(id), nil
}

func (h *Admin) Store() *store.Store { return h.st }

func (h *Admin) base(c *gin.Context, title string) gin.H {
	currentPostPermalink := syncPostPermalink(c, h.st)
	// 批量查询设置，避免多次 GetSetting
	settings, err := h.st.GetSettings(c, consts.SettingsSiteName, consts.SettingsDefaultAvatar, consts.SettingsShowSQLDetails)
	if err != nil && h.log != nil {
		h.log.Error("get settings", "error", err)
	}
	v := settings[consts.SettingsSiteName]
	defaultAvatar := settings[consts.SettingsDefaultAvatar]
	pendingCount := h.st.PendingCommentCount(c)
	// 缓存 pending 数到 context，避免 DashboardStats 中 AdminCommentCounts 重复查询
	c.Request = c.Request.WithContext(store.CtxWithPendingCommentCount(c.Request.Context(), pendingCount))
	siteName := util.FirstNonEmptyOr(consts.SettingsSiteNameDefault, v)
	data := gin.H{
		"SiteName":             siteName,
		"Title":                title,
		"DefaultAvatar":        util.NormalizeDefaultAvatar(defaultAvatar),
		"PendingCount":         pendingCount,
		"PostPermalinkPattern": currentPostPermalink,
		"AssetVersion":         assetVersion(),
		"InstanceVersion":      version.Display(),
		"RoleAdmin":            model.RoleAdmin,
		"RoleAuthor":           model.RoleAuthor,
		"RoleSubscriber":       model.RoleSubscriber,
	}
	if h.themeManager != nil {
		if currentTheme := h.themeManager.Current(c); currentTheme != nil {
			data["AdminThemeSupportsWidgets"] = len(currentTheme.WidgetAreas) > 0
			data["AdminThemeSupportsOptions"] = len(currentTheme.Options) > 0
			data["AdminThemeSupportsMenus"] = len(currentTheme.MenuLocations) > 0
		}
	}
	if c != nil {
		currentUser := h.currentUser(c)
		data["User"] = currentUser
		if currentUser != nil {
			data["CurrentUserID"] = currentUser.ID
		}
		data["CSRFToken"] = middleware.CSRFToken(c)
		data["CurrentAdminNav"] = adminNavKey(c)
		s := sessions.Default(c)
		if role, ok := s.Get(middleware.SessionRoleKey).(string); ok {
			data["CurrentUserRole"] = role
		}
		// 管理员且设置开启时，注入 SQL 详情供模板 footer 输出
		if currentUser != nil && currentUser.Role == model.RoleAdmin {
			if settings[consts.SettingsShowSQLDetails] == "true" {
				data["SQLDetails"] = &store.LazySQLDetails{Ctx: c.Request.Context()}
			}
		}
	}
	return i18n.Inject(c, data)
}

func (h *Admin) currentUserRole(c *gin.Context) string {
	if u := h.currentUser(c); u != nil {
		return u.Role
	}
	if c != nil {
		if role, ok := sessions.Default(c).Get(middleware.SessionRoleKey).(string); ok {
			return role
		}
	}
	return ""
}

func (h *Admin) canManagePost(c *gin.Context, p *model.Post) bool {
	if p == nil {
		return false
	}
	switch h.currentUserRole(c) {
	case model.RoleAdmin:
		return true
	case model.RoleAuthor:
		return p.AuthorID == currentUserID(c)
	default:
		return false
	}
}

func (h *Admin) canManageComment(c *gin.Context, commentID uint) bool {
	if h.currentUserRole(c) == model.RoleAdmin {
		return true
	}
	if h.currentUserRole(c) != model.RoleAuthor {
		return false
	}
	comment, err := h.st.GetCommentByID(c, commentID)
	if err != nil {
		return false
	}
	post, err := h.st.PostMeta(c, comment.PostID)
	if err != nil {
		return false
	}
	return post.AuthorID == currentUserID(c)
}

func (h *Admin) filterManageableCommentIDs(c *gin.Context, ids []uint) []uint {
	if h.currentUserRole(c) == model.RoleAdmin {
		return ids
	}
	allowed := make([]uint, 0, len(ids))
	for _, id := range ids {
		if h.canManageComment(c, id) {
			allowed = append(allowed, id)
		}
	}
	return allowed
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
	case "/admin/settings", "/admin/settings/developer", "/admin/settings/site", "/admin/settings/session", "/admin/settings/assets/release", "/admin/settings/assets/embed", "/admin/settings/i18n/release", "/admin/settings/i18n/embed", "/admin/settings/templates/release", "/admin/settings/templates/embed", "/admin/settings/templates/reload", "/admin/settings/theme/reload", "/admin/settings/plugins/reload":
		return "settings"
	case "/admin/profile", "/admin/profile/password":
		return "profile"
	case "/admin/my-comments", "/admin/my-comments/:id/edit", "/admin/my-comments/:id/delete":
		return "profile-comments"
	case "/admin/export-data":
		return "profile-export"
	case "/admin/delete-account":
		return "profile-delete"
	case "/admin/debug":
		return "debug"
	case "/admin/uploads", "/admin/uploads.json", "/admin/upload", "/admin/upload/:id/delete":
		return "uploads"
	case "/admin/import", "/admin/export":
		return "import"
	case "/admin/users", "/admin/user/:id/role", "/admin/user/:id/delete":
		return "users"
	case "/admin/themes", "/admin/theme/upload", "/admin/theme/activate", "/admin/theme/delete", "/admin/theme/download", "/admin/theme/preview", "/admin/theme/preview/clear", "/admin/theme/screenshot/:name/:file":
		return "themes"
	case "/admin/theme/files", "/admin/theme/file", "/admin/theme/file/create", "/admin/theme/file/delete", "/admin/theme/file/reload", "/admin/theme/recovery/clear", "/admin/theme/reload":
		return "theme-files"
	case "/admin/menus":
		return "menus"
	case "/admin/widgets":
		return "widgets"
	case "/admin/theme-options":
		return "theme-options"
	case "/admin/plugins", "/admin/plugin/:id/:action", "/admin/plugin/:id/settings":
		return "plugins"
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

// safeRedirect 校验 Referer 是否为本域名,是则重定向到 Referer,否则回退到 fallback。
func safeRedirect(c *gin.Context, fallback string) {
	ref := strings.TrimSpace(c.GetHeader("Referer"))
	if ref != "" {
		u, err := url.Parse(ref)
		if err == nil && u.Host != "" && strings.EqualFold(u.Host, c.Request.Host) {
			c.Redirect(http.StatusSeeOther, ref)
			return
		}
	}
	c.Redirect(http.StatusSeeOther, fallback)
}

func (h *Admin) currentUser(c *gin.Context) *model.User {
	return currentUserByStore(c, h.st, c)
}

func (h *Admin) currentTheme(c *gin.Context) *theme.Theme {
	if h.themeManager == nil {
		return nil
	}
	return h.themeManager.Current(c)
}

func normalizeTermSlug(s string) string {
	return util.Slugify(s)
}

func normalizeTaxonomySlug(s string) string {
	return util.URLSlugify(s)
}

func (h *Admin) notFound(c *gin.Context) {
	tr := i18n.Get(c)
	c.HTML(http.StatusNotFound, "admin_error.gohtml", i18n.Inject(c, gin.H{
		"Title":        "404",
		"Message":      tr.T("未找到"),
		"AssetVersion": assetVersion(),
	}))
}

func (h *Admin) serverError(c *gin.Context, err error) {
	h.log.Error("admin error", slog.Any("error", err), slog.String("path", c.Request.URL.Path))
	tr := i18n.Get(c)
	c.HTML(http.StatusInternalServerError, "admin_error.gohtml", i18n.Inject(c, gin.H{
		"Title":        "500",
		"Message":      tr.T("服务器错误"),
		"AssetVersion": assetVersion(),
	}))
}

func assetVersion() string {
	root := filepath.Join("web", "assets")
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return version.Version
	}
	maxMod := info.ModTime().UnixNano()
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if mod := info.ModTime().UnixNano(); mod > maxMod {
			maxMod = mod
		}
		return nil
	})
	return version.Version + "-" + strconv.FormatInt(maxMod, 36)
}

var usernameAllowedRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{2,32}$`)

func validateUsernameT(tr func(string, ...any) string, username string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New(tr(gettext.Mark.T("用户名不能为空")))
	}
	if !usernameAllowedRe.MatchString(username) {
		return errors.New(tr(gettext.Mark.T("用户名仅支持字母、数字、下划线和连字符，长度 2-32 个字符")))
	}
	return nil
}
