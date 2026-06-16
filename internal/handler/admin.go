package handler

import (
	"log/slog"
	"net/http"
	"net/url"
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
	renderx "github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"
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

func (h *Admin) base(c *gin.Context, title string) gin.H {
	currentPostPermalink := syncPostPermalink(c, h.st)
	v, err := h.st.GetSetting(c, consts.SettingsSiteName)
	if err != nil && h.log != nil {
		h.log.Error("get site name setting", "error", err)
	}
	defaultAvatar, err := h.st.GetSetting(c, consts.SettingsDefaultAvatar)
	if err != nil && h.log != nil {
		h.log.Error("get default avatar setting", "error", err)
	}
	siteName := util.FirstNonEmptyOr(consts.SettingsSiteNameDefault, v)
	data := gin.H{
		"SiteName":             siteName,
		"Title":                title,
		"DefaultAvatar":        util.NormalizeDefaultAvatar(defaultAvatar),
		"PendingCount":         h.st.PendingCommentCount(c),
		"PostPermalinkPattern": currentPostPermalink,
		"RoleAdmin":            model.RoleAdmin,
		"RoleAuthor":           model.RoleAuthor,
		"RoleSubscriber":       model.RoleSubscriber,
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
	case "/admin/settings", "/admin/settings/developer", "/admin/settings/site", "/admin/settings/session", "/admin/settings/assets/release", "/admin/settings/assets/embed", "/admin/settings/i18n/release", "/admin/settings/i18n/embed", "/admin/settings/templates/release", "/admin/settings/templates/embed", "/admin/settings/templates/reload":
		return "settings"
	case "/admin/profile", "/admin/profile/password":
		return "profile"
	case "/admin/my-comments", "/admin/my-comments/:id/delete":
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

func normalizeTermSlug(s string) string {
	return util.Slugify(s)
}

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
