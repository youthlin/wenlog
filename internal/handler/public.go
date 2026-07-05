// Package handler 提供 gin 的前台与后台 HTTP 处理器。
package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/wenlog/internal/config"
	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/i18n"
	"github.com/youthlin/wenlog/internal/middleware"
	"github.com/youthlin/wenlog/internal/model"
	"github.com/youthlin/wenlog/internal/permalink"
	"github.com/youthlin/wenlog/internal/render"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/theme"
	"github.com/youthlin/wenlog/internal/util"
)

// Public 是前台处理器。
type Public struct {
	st           *store.Store
	cfg          *config.Config
	log          *slog.Logger
	themeManager *theme.Manager
	renderer     *render.Renderer
}

const commentPageSize = 20

type publicSettings struct {
	SiteName         string
	SiteDescription  string
	SiteLogo         string
	PostPermalink    string
	CategoryPrefix   string
	TagPrefix        string
	PageSize         int
	FeedSize         int
	DefaultAvatar    string
	RegistrationOpen bool
	MailEnabled      bool
	ShowSQLDetails   bool
}

// NewPublic 构造前台处理器。
func NewPublic(st *store.Store, cfg *config.Config, tm *theme.Manager, renderer *render.Renderer) *Public {
	return &Public{
		st:           st,
		cfg:          cfg,
		log:          slog.Default().With("component", "public-handler"),
		themeManager: tm,
		renderer:     renderer,
	}
}

// Index 首页
func (h *Public) Index(c *gin.Context) {
	var loader = h.loadCache(c)
	if loader == nil {
		return
	}

	page := atoiDefault(c.Query("page"), 1)
	s := h.loadSettingsFromLoader(loader)
	res := loader.ListPosts(page, s.PageSize, "", "")

	// 从 loader 读主题名，避免 current_theme 查 DB
	data := h.base(c, s.SiteName, "", s, loader)
	data["List"] = res
	data["Pager"] = pager(res, "/")
	h.renderPageHTML(c, http.StatusOK, "index", data)
}

func (h *Public) loadCache(c *gin.Context) *store.DataLoader {
	// 全量预加载，后续所有数据从内存读取
	loader, err := h.st.LoadAllCached(c)
	if err != nil {
		h.serverError(c, err)
		return nil
	}
	syncPostPermalinkFromLoader(loader)
	return loader
}

// renderHTML 渲染模板，管理员预览主题时使用预览模板，否则使用主模板。
func (h *Public) renderHTML(c *gin.Context, code int, name string, data gin.H) {
	previewName := middleware.GetPreviewTheme(c)
	c.Render(code, h.renderer.PreviewInstance(name, data, previewName))
}

// renderPageHTML 按当前/预览主题的模板层级解析页面类型并渲染。
func (h *Public) renderPageHTML(c *gin.Context, code int, pageType string, data gin.H) {
	if data != nil {
		data["PageType"] = pageType
	}
	previewName := middleware.GetPreviewTheme(c)
	name := h.renderer.ResolveTemplateWithPreviewData(pageType, previewName, data)
	c.Render(code, h.renderer.PreviewInstance(name, data, previewName))
}

// PostIDRedirect 处理 /?p={id} 旧链接 301 到永久链接。
// 返回 true 表示已处理。
func (h *Public) PostIDRedirect(c *gin.Context, pid string) bool {
	var loader = h.loadCache(c)
	if loader == nil {
		return true
	}
	if pid == "" {
		return false
	}
	postID, err := strconv.ParseUint(pid, 10, 64)
	if err != nil {
		return false
	}
	id := uint(postID)

	if p := loader.GetPostByID(id); p != nil {
		if p.Status != model.StatusPublished {
			if d := h.draftForAuthor(c, id); d == nil {
				return false
			}
		}
		c.Redirect(http.StatusMovedPermanently, permalink.Post(p))
		return true
	}
	// 内存中不存在，草稿也查一下
	if d := h.draftForAuthor(c, id); d != nil {
		c.Redirect(http.StatusMovedPermanently, permalink.Post(d))
		return true
	}
	return false
}

// Search 全文搜索(标题/正文子串匹配，使用 DataLoader 内存搜索)。
func (h *Public) Search(c *gin.Context) {
	var loader = h.loadCache(c)
	if loader == nil {
		return
	}

	kw := strings.TrimSpace(c.Query("q"))
	page := atoiDefault(c.Query("page"), 1)

	s := h.loadSettingsFromLoader(loader)
	var res *store.ListPostsResult
	if kw == "" {
		res = &store.ListPostsResult{Page: 1}
	} else {
		res = loader.SearchPosts(kw, page, s.PageSize)
	}
	tr := i18n.Get(c)
	data := h.base(c, tr.T("搜索：%s", kw), "", s, loader)
	data["Heading"] = tr.T("搜索「%s」", kw)
	data["Keyword"] = kw
	data["List"] = res
	data["Pager"] = pager(res, "/search?q="+url.QueryEscape(kw))
	h.renderPageHTML(c, http.StatusOK, "search", data)
}

// DynamicOrLegacy 是前台兜底路由：页面、文章固定链接与旧链接兼容都在这里收口。
func (h *Public) DynamicOrLegacy(c *gin.Context) {
	var loader = h.loadCache(c)
	if loader == nil {
		return
	}

	path := c.Request.URL.EscapedPath()

	if slug, ok := singleSegmentSlug(path); ok {
		if h.pageExistsFromLoader(loader, slug) {
			c.Params = append(c.Params, gin.Param{Key: "slug", Value: slug})
			h.pageWithLoader(c, loader)
			return
		}
	}
	if slug, ok := permalink.ParseCategoryPath(path); ok {
		c.Params = append(c.Params, gin.Param{Key: "slug", Value: slug})
		h.categoryWithLoader(c, loader)
		return
	}
	if slug, ok := permalink.ParseTagPath(path); ok {
		c.Params = append(c.Params, gin.Param{Key: "slug", Value: slug})
		h.tagWithLoader(c, loader)
		return
	}
	if match, ok := permalink.ParsePostPath(path); ok {
		if h.renderResolvedPostWithLoader(c, path, match, loader) {
			return
		}
	}
	h.notFound(c, loader)
}

// singleSegmentSlug 是否只有单段 slug
func singleSegmentSlug(path string) (string, bool) {
	path = strings.Trim(path, "/")
	if path == "" || strings.ContainsRune(path, '/') {
		return "", false
	}
	return path, true
}

// pageExistsFromLoader 是否有指定 slug 的页面
func (h *Public) pageExistsFromLoader(loader *store.DataLoader, slug string) bool {
	if slug == "archive" {
		return true
	}
	return loader.GetPageBySlug(slug) != nil
}

// pageWithLoader 输出页面
func (h *Public) pageWithLoader(c *gin.Context, loader *store.DataLoader) {
	slug := strings.Trim(c.Param("slug"), "/")
	if slug == "" {
		h.notFound(c, loader)
		return
	}
	if slug == "archive" {
		tr := i18n.Get(c)
		h.renderArchive(c, h.specialPageFromLoader(loader, "archive", tr.T("归档")), loader)
		return
	}
	p := loader.GetPageBySlug(slug)
	if p == nil {
		h.notFound(c, loader)
		return
	}
	if p.Status == model.StatusPublished {
		if err := h.st.IncrementViews(c, p.ID); err != nil && h.log != nil {
			h.log.Error("增加页面浏览量失败", "error", err, "post_id", p.ID)
		}
		p.Views++
	}
	h.renderContentPost(c, p, loader, "page", permalink.Page(p), nil)
}

func (h *Public) specialPageFromLoader(loader *store.DataLoader, slug, fallbackTitle string) *model.Post {
	if p := loader.GetPageBySlug(slug); p != nil {
		return p
	}
	return &model.Post{Title: fallbackTitle, Slug: slug, PostType: model.PostTypePage}
}

func (h *Public) renderArchive(c *gin.Context, p *model.Post, loader *store.DataLoader) {
	posts := loader.AllPostsForArchive()
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

	s := h.loadSettingsFromLoader(loader)
	data := h.base(c, p.Title, "", s, loader)
	data["Post"] = p
	data["Groups"] = groups
	h.renderPageHTML(c, http.StatusOK, "archive", data)
}

func (h *Public) categoryWithLoader(c *gin.Context, loader *store.DataLoader) {
	displaySlug := c.Param("slug")
	if displaySlug == "" {
		displaySlug = c.Query("slug")
	}
	slug := encodeTaxonomySlug(displaySlug)
	page := atoiDefault(c.Query("page"), 1)

	s := h.loadSettingsFromLoader(loader)
	res := loader.ListPosts(page, s.PageSize, slug, "")
	tr := i18n.Get(c)
	data := h.base(c, tr.T("分类：%s", displaySlug), "", s, loader)
	data["Heading"] = tr.T("分类：%s", displaySlug)
	data["TermSlug"] = slug
	data["CategorySlug"] = slug
	data["List"] = res
	data["Pager"] = pager(res, permalink.Category(slug))
	h.renderPageHTML(c, http.StatusOK, "category", data)
}

func (h *Public) tagWithLoader(c *gin.Context, loader *store.DataLoader) {
	displaySlug := c.Param("slug")
	if displaySlug == "" {
		displaySlug = c.Query("slug")
	}
	slug := encodeTaxonomySlug(displaySlug)
	page := atoiDefault(c.Query("page"), 1)

	s := h.loadSettingsFromLoader(loader)
	res := loader.ListPosts(page, s.PageSize, "", slug)
	tr := i18n.Get(c)
	data := h.base(c, tr.T("标签：%s", displaySlug), "", s, loader)
	data["Heading"] = tr.T("标签：%s", displaySlug)
	data["TermSlug"] = slug
	data["TagSlug"] = slug
	data["List"] = res
	data["Pager"] = pager(res, permalink.Tag(slug))
	h.renderPageHTML(c, http.StatusOK, "tag", data)
}

// renderResolvedPostWithLoader 处理文章
func (h *Public) renderResolvedPostWithLoader(c *gin.Context, path string, match *permalink.PostPathMatch, loader *store.DataLoader) bool {
	p, err := loader.ResolvePostByPath(path, match)
	if err != nil {
		if match == nil || !match.HasPostID {
			return false
		}
		p = h.draftForAuthor(c, match.PostID)
		if p == nil {
			return false
		}
	}
	if got := permalink.Post(p); got != path {
		c.Redirect(http.StatusMovedPermanently, got)
		return true
	}
	if p.Status == model.StatusPublished {
		if err := h.st.IncrementViews(c, p.ID); err != nil && h.log != nil {
			h.log.Error("增加文章浏览量失败", "error", err, "post_id", p.ID)
		}
		p.Views++
	}
	loader.FillPost(p)
	extra := gin.H{
		"PrevPost": loader.PrevPost(p.PublishedAt),
		"NextPost": loader.NextPost(p.PublishedAt),
	}
	h.renderContentPost(c, p, loader, "post", permalink.Post(p), extra)
	return true
}

// renderContentPost 加载评论、拼装模板数据并渲染文章/页面。
// extra 中的字段会合并到模板数据中（如 PrevPost/NextPost）。
func (h *Public) renderContentPost(c *gin.Context, p *model.Post, loader *store.DataLoader, templateName, baseURL string, extra gin.H) {
	commentPage := atoiDefault(c.Query("cpage"), 1)
	var comments *store.CommentPageResult
	uid := currentUserID(c)
	pendingIDs := pendingCommentIDs(c)
	if uid != 0 {
		comments = loader.CommentPage(p.ID, commentPage, commentPageSize, uid)
	} else if len(pendingIDs) == 0 {
		comments = loader.CommentPage(p.ID, commentPage, commentPageSize, 0)
	} else {
		var err error
		comments, err = h.st.VisibleCommentsPageForViewer(c, p.ID, commentPage, commentPageSize, uid, pendingIDs)
		if err != nil {
			h.serverError(c, err)
			return
		}
	}
	s := h.loadSettingsFromLoader(loader)
	data := h.base(c, p.Title, p.Excerpt, s, loader)
	data["CanonicalURL"] = requestBaseURL(c) + baseURL
	data["Post"] = p
	data["Comments"] = comments.Comments
	data["CommentPager"] = gin.H{"Page": comments.Page, "Pages": comments.Pages, "BaseURL": baseURL, "Sep": "?"}
	data["CommentCount"] = comments.TotalComments
	data["CommentOpen"] = p.CommentStatus != "closed"
	data["RememberedCommenter"] = rememberedCommenter(c)
	for k, v := range extra {
		data[k] = v
	}
	if fragName, ok := h.renderer.ResolveFragment(c.Query("fragment")); ok {
		h.renderHTML(c, http.StatusOK, fragName, data)
		return
	}
	h.renderPageHTML(c, http.StatusOK, templateName, data)
}

// base 返回模板通用数据(站点名、菜单、当前年份、当前登录用户)。
// 所有常用前台数据始终注入，主题无需声明 data 依赖。
// loader 为全量预加载的数据，前台页面统一从 loader 取公共数据，避免模板渲染期反复查库。
func (h *Public) base(c *gin.Context, title, desc string, s publicSettings, loader *store.DataLoader) gin.H {
	menu := loader.MenuPages()
	if strings.TrimSpace(desc) == "" {
		desc = s.SiteDescription
	}
	uid := currentUserID(c)
	currentUser := currentUserFromLoader(c, loader)
	csrfToken := ""
	if uid != 0 {
		if token, err := middleware.EnsureCSRFToken(c); err == nil {
			csrfToken = token
		}
	}
	// 缓存当前主题和版本号。
	currentTheme := h.currentThemeFromLoader(c, loader)
	var themeVersion string
	menus := templateMenus(h.resolveMenus(c, currentTheme, menu, loader))
	data := gin.H{
		"SiteName":         s.SiteName,
		"SiteLogo":         s.SiteLogo,
		"AssetVersion":     assetVersion(),
		"Title":            title,
		"Description":      desc,
		"CanonicalURL":     requestBaseURL(c) + c.Request.URL.String(),
		"Menu":             menu,
		"Menus":            menus,
		"CurrentYear":      time.Now().Year(),
		"CurrentUserID":    uid,
		"CurrentUser":      currentUser,
		"DefaultAvatar":    s.DefaultAvatar,
		"CSRFToken":        csrfToken,
		"RegistrationOpen": s.RegistrationOpen,
		"MailEnabled":      s.MailEnabled,
	}
	data[render.ContextDataKey] = c.Request.Context()
	data[render.ThemeLoaderDataKey] = loader

	// 所有常用数据始终注入，不再按需查询
	data["RecentPosts"] = loader.RecentPosts(20)
	data["Categories"] = loader.AllCategories()
	data["Tags"] = loader.AllTags()
	data["ArchiveMonths"] = loader.ArchiveMonths()
	recentComments := loader.RecentComments(8)
	data["RecentCommentItems"] = loader.CommentWidgetItems(recentComments)

	themeName := ""
	if currentTheme != nil {
		themeName = currentTheme.Name
		themeVersion = currentTheme.Version
		data[render.ThemeDataKey] = currentTheme
	}
	data["CurrentTheme"] = currentTheme
	data["ThemeName"] = themeName
	data["ThemeVersion"] = themeVersion

	// 补充模板需要的字段
	data["Keyword"] = c.Query("q")
	if currentUser != nil {
		data["CurrentUserName"] = displayUserName(currentUser)
	} else {
		data["CurrentUserName"] = ""
	}
	// 管理员且设置开启时，注入 SQL 详情供模板 footer 输出
	if currentUser != nil && currentUser.Role == model.RoleAdmin && s.ShowSQLDetails {
		data["SQLDetails"] = &store.LazySQLDetails{Ctx: c.Request.Context()}
	}
	themeDomain := ""
	if currentTheme != nil {
		themeDomain = currentTheme.ThemeDomain()
	}
	return i18n.InjectDomain(c, data, themeDomain)
}

func (h *Public) resolveMenus(c *gin.Context, currentTheme *theme.Theme, fallback []model.Post, loader *store.DataLoader) map[string][]theme.MenuItem {
	locations := theme.MenuLocationEntries{{Key: "primary", MenuLocation: theme.MenuLocation{Name: "主导航"}}}
	if currentTheme != nil && len(currentTheme.MenuLocations) > 0 {
		locations = currentTheme.MenuLocations
	}
	menus := make(map[string][]theme.MenuItem, len(locations))
	for _, entry := range locations {
		raw := loader.GetSetting(theme.MenuSettingKey(entry.Key))
		menus[entry.Key] = theme.ResolveMenuItems(raw, fallback)
	}
	return menus
}

func templateMenus(menus map[string][]theme.MenuItem) map[string]any {
	if len(menus) == 0 {
		return nil
	}
	result := make(map[string]any, len(menus))
	for location, items := range menus {
		out := make([]any, 0, len(items))
		for i := range items {
			out = append(out, items[i])
		}
		result[location] = out
	}
	return result
}

func displayUserName(u *model.User) string {
	if u == nil {
		return ""
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func (h *Public) mailEnabled(ctx context.Context) bool {
	return smtpConfigFromStore(ctx, h.st).Configured()
}

// currentThemeFromLoader 从 DataLoader 内存中读取 current_theme，不查 DB。
// 同时支持管理员主题预览（从 session 读取）。
func (h *Public) currentThemeFromLoader(c *gin.Context, loader *store.DataLoader) *theme.Theme {
	if h.themeManager == nil {
		return nil
	}
	// 管理员主题预览：从 session 读取预览主题名
	if previewName := middleware.GetPreviewTheme(c); previewName != "" {
		if t := h.themeManager.Get(previewName); t != nil {
			return t
		}
	}
	name := loader.GetSetting("current_theme")
	if name == "" {
		name = "default"
	}
	return h.themeManager.Get(name)
}

func (h *Public) loadSettings(ctx context.Context) publicSettings {
	settings, err := h.st.GetSettings(ctx, consts.SettingsSiteName,
		consts.SettingsSiteDesc,
		consts.SettingsSiteLogo,
		consts.SettingsPageSize,
		consts.SettingsFeedSize,
		consts.SettingsDefaultAvatar,
		consts.SettingsRegistrationOpen,
		consts.SettingsSMTPHost,
		consts.SettingsSMTPPort,
		consts.SettingsSMTPUser,
		consts.SettingsSMTPPassword,
		consts.SettingsSMTPFrom,
		consts.SettingsShowSQLDetails,
	)
	if err != nil && h.log != nil {
		h.log.Error("load public settings", "error", err)
	}
	return h.buildSettings(settings)
}

func (h *Public) loadSettingsFromLoader(loader *store.DataLoader) publicSettings {
	settings := loader.GetSettings(consts.SettingsSiteName,
		consts.SettingsSiteDesc,
		consts.SettingsSiteLogo,
		consts.SettingsPageSize,
		consts.SettingsFeedSize,
		consts.SettingsDefaultAvatar,
		consts.SettingsRegistrationOpen,
		consts.SettingsSMTPHost,
		consts.SettingsSMTPPort,
		consts.SettingsSMTPUser,
		consts.SettingsSMTPPassword,
		consts.SettingsSMTPFrom,
		consts.SettingsShowSQLDetails,
	)
	return h.buildSettings(settings)
}

func (h *Public) buildSettings(settings map[string]string) publicSettings {
	return publicSettings{
		SiteName:         util.FirstNonEmpty(settings[consts.SettingsSiteName], consts.SettingsSiteNameDefault),
		SiteDescription:  settings[consts.SettingsSiteDesc],
		SiteLogo:         settings[consts.SettingsSiteLogo],
		PostPermalink:    permalink.CurrentPostPattern(),
		CategoryPrefix:   permalink.CurrentCategoryPrefix(),
		TagPrefix:        permalink.CurrentTagPrefix(),
		PageSize:         positiveIntSetting(settings[consts.SettingsPageSize], defaultPublicPageSize),
		FeedSize:         positiveIntSetting(settings[consts.SettingsFeedSize], defaultFeedSize),
		DefaultAvatar:    util.NormalizeDefaultAvatar(settings[consts.SettingsDefaultAvatar]),
		RegistrationOpen: settings[consts.SettingsRegistrationOpen] == "true",
		MailEnabled:      smtpConfigFromSettings(settings).Configured(),
		ShowSQLDetails:   settings[consts.SettingsShowSQLDetails] == "true",
	}
}

// draftForAuthor 返回草稿文章,仅当请求者是该文章作者本人;否则 nil。
func (h *Public) draftForAuthor(c *gin.Context, id uint) *model.Post {
	uid := currentUserID(c)
	if uid == 0 {
		return nil
	}
	p, err := h.st.GetPostAnyStatus(c, id)
	if err != nil || p.Status != model.StatusDraft || p.AuthorID != uid {
		return nil
	}
	return p
}

func (h *Public) NotFound(c *gin.Context) {
	loader := h.loadCache(c)
	if loader == nil {
		return
	}
	h.notFound(c, loader)
}

// notFound 渲染 404 页面。
func (h *Public) notFound(c *gin.Context, loader *store.DataLoader) {
	tr := i18n.Get(c)
	s := h.loadSettingsFromLoader(loader)
	data := h.base(c, tr.T("页面不存在"), "", s, loader)
	data["Code"] = 404
	data["Message"] = tr.T("你访问的页面不存在或已被删除。")
	h.renderPageHTML(c, http.StatusNotFound, "error", data)
}

func (h *Public) serverError(c *gin.Context, err error) {
	h.log.ErrorContext(c, "服务器处理失败",
		slog.String("path", c.Request.URL.Path),
		slog.Any("error", err),
	)
	h.textError(c, http.StatusInternalServerError, "服务器内部错误")
}

func (h *Public) textError(c *gin.Context, code int, msg string) {
	c.String(code, "%d %s", code, msg)
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

func encodeTaxonomySlug(slug string) string {
	return util.URLSlugify(slug)
}
