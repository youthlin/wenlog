// Package handler 提供 gin 的前台与后台 HTTP 处理器。
package handler

import (
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/config"
	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/store"
)

// Public 是前台处理器。
type Public struct {
	st  *store.Store
	cfg *config.Config
	log *slog.Logger
}

const commentPageSize = 20

type publicSettings struct {
	SiteName        string
	SiteDescription string
	PageSize        int
	FeedSize        int
}

// NewPublic 构造前台处理器。
func NewPublic(st *store.Store, cfg *config.Config, log *slog.Logger) *Public {
	return &Public{st: st, cfg: cfg, log: log}
}

// base 返回模板通用数据(站点名、菜单、当前年份、侧栏小组件、当前登录用户)。
func (h *Public) base(c *gin.Context, title, desc string, s publicSettings) gin.H {
	menu, _ := h.st.MenuPages()
	if strings.TrimSpace(desc) == "" {
		desc = s.SiteDescription
	}
	currentUserID := h.currentUserID(c)
	currentUser := h.currentUser(c)
	csrfToken := ""
	if currentUserID != 0 {
		if token, err := middleware.EnsureCSRFToken(c); err == nil {
			csrfToken = token
		}
	}
	return i18n.Inject(c, gin.H{
		"SiteName":           s.SiteName,
		"Title":              title,
		"Description":        desc,
		"Menu":               menu,
		"CurrentYear":        currentYear(),
		"CurrentUserID":      currentUserID,
		"CurrentUser":        currentUser,
		"CSRFToken":          csrfToken,
		"RecentComments":     h.st.RecentComments(8),
		"RecentCommentItems": h.st.RecentCommentItems(8, commentPageSize),
		"RecentPosts":        h.st.RecentPosts(8),
		"SayingComments":     h.st.SayingComments(5),
		"SayingCommentItems": h.st.SayingCommentItems(5, commentPageSize),
		"ArchiveMonths":      h.st.ArchiveMonths(),
		"Categories":         h.st.AllCategories(),
		"Tags":               h.st.AllTags(),
	})
}

func (h *Public) loadSettings() publicSettings {
	settings, _ := h.st.GetSettings(
		consts.SettingsSiteName,
		consts.SettingsSiteDesc,
		consts.SettingsPageSize,
		consts.SettingsFeedSize,
	)
	return publicSettings{
		SiteName:        firstNonEmpty(settings[consts.SettingsSiteName], consts.SettingsSiteNameDefault),
		SiteDescription: settings[consts.SettingsSiteDesc],
		PageSize:        positiveIntSetting(settings[consts.SettingsPageSize], defaultPublicPageSize),
		FeedSize:        positiveIntSetting(settings[consts.SettingsFeedSize], defaultFeedSize),
	}
}

// currentUser 返回当前登录用户(未登录为 nil)。
func (h *Public) currentUser(c *gin.Context) *model.User {
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

// currentUserID 返回当前登录用户 ID(未登录为 0)。
func (h *Public) currentUserID(c *gin.Context) uint {
	s := sessions.Default(c)
	if v := s.Get(middleware.SessionUserKey); v != nil {
		if id, ok := v.(uint); ok {
			return id
		}
	}
	return 0
}

// Index 首页文章列表。
func (h *Public) Index(c *gin.Context) {
	page := atoiDefault(c.Query("page"), 1)
	s := h.loadSettings()
	res, err := h.st.ListPosts(page, s.PageSize, "", "")
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, s.SiteName, "", s)
	data["List"] = res
	data["Pager"] = pager(res, "/")
	c.HTML(http.StatusOK, "index.gohtml", data)
}

// Search 全文搜索(标题/正文 LIKE)。
func (h *Public) Search(c *gin.Context) {
	kw := strings.TrimSpace(c.Query("q"))
	page := atoiDefault(c.Query("page"), 1)
	s := h.loadSettings()
	var res *store.ListPostsResult
	var err error
	if kw == "" {
		res = &store.ListPostsResult{Page: 1}
	} else {
		res, err = h.st.SearchPosts(kw, page, s.PageSize)
		if err != nil {
			h.serverError(c, err)
			return
		}
	}
	tr := i18n.Get(c)
	data := h.base(c, tr.T("搜索:%s", kw), "", s)
	data["Heading"] = tr.T("搜索「%s」", kw)
	data["Keyword"] = kw
	data["List"] = res
	data["Pager"] = pager(res, "/search?q="+url.QueryEscape(kw))
	c.HTML(http.StatusOK, "list.gohtml", data)
}

// Post 文章详情(永久链接 /{year}{id}.html)。
func (h *Public) Post(c *gin.Context) {
	year, id, ok := permalink.ParsePostPath(c.Request.URL.Path)
	if !ok {
		h.notFound(c)
		return
	}
	p, err := h.st.GetPostByID(id)
	if err != nil {
		// 已发布未命中:可能是草稿,仅作者本人可预览。
		p = h.draftForAuthor(c, id)
		if p == nil {
			h.notFound(c)
			return
		}
	}
	// 校验 URL 中年份与发布年份一致,否则 301 到正确链接(保链接健壮性)。
	if p.PublishedAt.Year() != year {
		c.Redirect(http.StatusMovedPermanently, permalink.Post(p))
		return
	}
	if p.Status == model.StatusPublished {
		_ = h.st.IncrementViews(id)
		p.Views++ // 本次展示也算上
	}

	commentPage := atoiDefault(c.Query("cpage"), 1)
	comments, err := h.st.ApprovedCommentsPage(id, commentPage, commentPageSize)
	if err != nil {
		h.serverError(c, err)
		return
	}
	s := h.loadSettings()
	data := h.base(c, p.Title, p.Excerpt, s)
	data["Post"] = p
	data["IsDraft"] = p.Status == model.StatusDraft
	data["Comments"] = comments.Comments
	data["CommentPager"] = gin.H{"Page": comments.Page, "Pages": comments.Pages, "BaseURL": permalink.Post(p), "Sep": "?"}
	data["CommentCount"] = comments.TotalComments
	data["CommentOpen"] = p.CommentStatus != "closed"
	data["PrevPost"] = h.st.PrevPost(p.PublishedAt)
	data["NextPost"] = h.st.NextPost(p.PublishedAt)
	if c.Query("ajax") == "comments" {
		c.HTML(http.StatusOK, "comments_fragment.gohtml", data)
		return
	}
	c.HTML(http.StatusOK, "post.gohtml", data)
}

// draftForAuthor 返回草稿文章,仅当请求者是该文章作者本人;否则 nil。
func (h *Public) draftForAuthor(c *gin.Context, id uint) *model.Post {
	uid := h.currentUserID(c)
	if uid == 0 {
		return nil
	}
	p, err := h.st.GetPostAnyStatus(id)
	if err != nil || p.Status != model.StatusDraft || p.AuthorID != uid {
		return nil
	}
	return p
}

// Page 处理 /{slug} 页面以及若干特殊页面(归档)。
func (h *Public) Page(c *gin.Context) {
	slug := strings.Trim(c.Param("slug"), "/")
	if slug == "" {
		h.notFound(c)
		return
	}
	if slug == "archive" {
		h.renderArchive(c, h.specialPage(c, "archive", "归档"))
		return
	}
	p, err := h.st.GetPageBySlug(slug)
	if err != nil {
		h.notFound(c)
		return
	}

	commentPage := atoiDefault(c.Query("cpage"), 1)
	comments, err := h.st.ApprovedCommentsPage(p.ID, commentPage, commentPageSize)
	if err != nil {
		h.serverError(c, err)
		return
	}
	s := h.loadSettings()
	data := h.base(c, p.Title, p.Excerpt, s)
	data["Post"] = p
	data["Comments"] = comments.Comments
	data["CommentPager"] = gin.H{"Page": comments.Page, "Pages": comments.Pages, "BaseURL": permalink.Page(p), "Sep": "?"}
	data["CommentCount"] = comments.TotalComments
	data["CommentOpen"] = p.CommentStatus != "closed"
	data["ShowComments"] = p.CommentStatus != "closed"
	if c.Query("ajax") == "comments" {
		c.HTML(http.StatusOK, "comments_fragment.gohtml", data)
		return
	}
	c.HTML(http.StatusOK, "page.gohtml", data)
}

func (h *Public) specialPage(c *gin.Context, slug, fallbackTitle string) *model.Post {
	if h.st != nil {
		p, err := h.st.GetPageBySlug(slug)
		if err == nil {
			return p
		}
	}
	tr := i18n.Get(c)
	return &model.Post{Title: tr.T(fallbackTitle), Slug: slug, PostType: model.PostTypePage}
}

func (h *Public) renderArchive(c *gin.Context, p *model.Post) {
	posts, err := h.st.AllPostsForArchive()
	if err != nil {
		h.serverError(c, err)
		return
	}
	// 按年份分组(已按发布时间倒序)。
	type group struct {
		Year  int
		Posts []model.Post
	}
	var groups []group
	idx := map[int]int{}
	for _, post := range posts {
		y := post.PublishedAt.Year()
		if i, ok := idx[y]; ok {
			groups[i].Posts = append(groups[i].Posts, post)
		} else {
			idx[y] = len(groups)
			groups = append(groups, group{Year: y, Posts: []model.Post{post}})
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Year > groups[j].Year })

	s := h.loadSettings()
	data := h.base(c, p.Title, "", s)
	data["Post"] = p
	data["Groups"] = groups
	c.HTML(http.StatusOK, "archive.gohtml", data)
}
// Category 分类列表页。
func (h *Public) Category(c *gin.Context) {
	slug := c.Param("slug")
	page := atoiDefault(c.Query("page"), 1)
	s := h.loadSettings()
	tr := i18n.Get(c)
	res, err := h.st.ListPosts(page, s.PageSize, slug, "")
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, tr.T("分类:%s", slug), "", s)
	data["Heading"] = tr.T("分类:%s", slug)
	data["List"] = res
	data["Pager"] = pager(res, permalink.Category(slug))
	c.HTML(http.StatusOK, "list.gohtml", data)
}

// Tag 标签列表页。
func (h *Public) Tag(c *gin.Context) {
	slug := c.Param("slug")
	page := atoiDefault(c.Query("page"), 1)
	s := h.loadSettings()
	tr := i18n.Get(c)
	res, err := h.st.ListPosts(page, s.PageSize, "", slug)
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, tr.T("标签:%s", slug), "", s)
	data["Heading"] = tr.T("标签:%s", slug)
	data["List"] = res
	data["Pager"] = pager(res, permalink.Tag(slug))
	c.HTML(http.StatusOK, "list.gohtml", data)
}

// LegacyQueryRedirect 处理 /?p={id} 旧链接 301 到永久链接。
// 返回 true 表示已处理。
func (h *Public) LegacyQueryRedirect(c *gin.Context) bool {
	pid := c.Query("p")
	if pid == "" {
		return false
	}
	id, err := strconv.ParseUint(pid, 10, 64)
	if err != nil {
		return false
	}
	p, err := h.st.PostMeta(uint(id))
	if err != nil {
		return false
	}
	// 草稿:仅作者本人可预览,跳到永久链接。
	if p.Status != model.StatusPublished {
		if d := h.draftForAuthor(c, uint(id)); d == nil {
			return false
		}
	}
	c.Redirect(http.StatusMovedPermanently, permalink.Post(p))
	return true
}

// notFound 渲染 404。
func (h *Public) notFound(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("页面不存在"), "", h.loadSettings())
	data["Code"] = 404
	data["Message"] = tr.T("你访问的页面不存在或已被删除。")
	c.HTML(http.StatusNotFound, "error.gohtml", data)
}

func (h *Public) serverError(c *gin.Context, err error) {
	h.log.Error("handler error", slog.Any("error", err), slog.String("path", c.Request.URL.Path))
	tr := i18n.Get(c)
	data := h.base(c, tr.T("出错了"), "", h.loadSettings())
	data["Code"] = 500
	data["Message"] = tr.T("服务器内部错误,请稍后重试。")
	c.HTML(http.StatusInternalServerError, "error.gohtml", data)
}

// NotFoundOrLegacy 是兜底路由:先尝试 /?p=ID,否则 404。
func (h *Public) NotFoundOrLegacy(c *gin.Context) {
	if h.LegacyQueryRedirect(c) {
		return
	}
	h.notFound(c)
}

// --- 辅助函数 ---

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}

// pager 构造分页模板数据。BaseURL 已含查询串时用 & 续接 page,否则用 ?。
func pager(res *store.ListPostsResult, baseURL string) gin.H {
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	return gin.H{"Page": res.Page, "Pages": res.Pages, "BaseURL": baseURL, "Sep": sep}
}

func currentYear() int { return time.Now().Year() }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
