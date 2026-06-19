// Package handler 提供 gin 的前台与后台 HTTP 处理器。
package handler

import (
	"bytes"
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/config"
	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/theme"
	"github.com/youthlin/blog/internal/util"
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
	PostPermalink    string
	CategoryPrefix   string
	TagPrefix        string
	PageSize         int
	FeedSize         int
	SayingPostID     uint
	DefaultAvatar    string
	RegistrationOpen bool
	ShowSQLDetails   bool
}

// NewPublic 构造前台处理器。
func NewPublic(st *store.Store, cfg *config.Config, log *slog.Logger, tm *theme.Manager, renderer *render.Renderer) *Public {
	return &Public{st: st, cfg: cfg, log: log, themeManager: tm, renderer: renderer}
}

// base 返回模板通用数据(站点名、菜单、当前年份、widget、当前登录用户)。
// pageCfg 来自 theme.yaml 的 pages.{page} 配置，为 nil 时查询所有数据（兼容无主题场景）。
// loader 为全量预加载的数据，为 nil 时回退到 store 查询（兼容后台等场景）。
func (h *Public) base(c *gin.Context, title, desc string, s publicSettings, pageCfg *theme.PageConfig, loader *store.DataLoader) gin.H {
	var menu []model.Post
	if loader != nil {
		menu = loader.MenuPages()
	} else {
		menu, _ = h.st.MenuPages(c)
	}
	if strings.TrimSpace(desc) == "" {
		desc = s.SiteDescription
	}
	uid := currentUserID(c)
	currentUser := h.currentUser(c)
	csrfToken := ""
	if uid != 0 {
		if token, err := middleware.EnsureCSRFToken(c); err == nil {
			csrfToken = token
		}
	}
	data := gin.H{
		"SiteName":         s.SiteName,
		"Title":            title,
		"Description":      desc,
		"Menu":             menu,
		"CurrentYear":      currentYear(),
		"CurrentUserID":    uid,
		"CurrentUser":      currentUser,
		"DefaultAvatar":    s.DefaultAvatar,
		"CSRFToken":        csrfToken,
		"RegistrationOpen": s.RegistrationOpen,
		"MailEnabled":      h.mailEnabled(),
	}

	// 按需查询数据（根据 theme.yaml 的 pages.{page}.data 声明）
	needs := func(field string) bool {
		if pageCfg == nil {
			return true // 无主题配置时查询所有
		}
		return pageCfg.Needs(field)
	}

	var recentPosts []model.Post
	var recentCommentItems []store.CommentWidgetItem
	var sayingCommentItems []store.CommentWidgetItem
	var archiveMonths []store.ArchiveMonth
	var categories []model.Category
	var tags []model.Tag

	if needs("RecentPosts") {
		if loader != nil {
			recentPosts = loader.RecentPosts(8)
		} else {
			recentPosts = h.st.RecentPosts(c, 8)
		}
		data["RecentPosts"] = recentPosts
	}
	if needs("RecentComments") {
		if loader != nil {
			comments := loader.RecentComments(8)
			recentCommentItems = loader.CommentWidgetItems(comments)
		} else {
			recentCommentItems = h.st.RecentCommentItems(c, 8, commentPageSize)
		}
		data["RecentCommentItems"] = recentCommentItems
	}
	if needs("SayingComments") {
		if loader != nil {
			comments := loader.SayingComments(s.SayingPostID, 5)
			sayingCommentItems = loader.CommentWidgetItems(comments)
		} else {
			sayingCommentItems = h.st.SayingCommentItems(c, s.SayingPostID, 5, commentPageSize)
		}
		data["SayingCommentItems"] = sayingCommentItems
		if s.SayingPostID > 0 {
			if len(sayingCommentItems) > 0 {
				data["SayingPost"] = &sayingCommentItems[0].Post
			} else if loader != nil {
				if p := loader.PostMeta(s.SayingPostID); p != nil {
					data["SayingPost"] = p
				}
			} else if p, err := h.st.PostMeta(c, s.SayingPostID); err == nil && p.Status == model.StatusPublished {
				data["SayingPost"] = p
			}
		}
	}
	if needs("ArchiveMonths") {
		if loader != nil {
			archiveMonths = loader.ArchiveMonths()
		} else {
			archiveMonths = h.st.ArchiveMonths(c)
		}
		data["ArchiveMonths"] = archiveMonths
	}
	if needs("Categories") {
		if loader != nil {
			categories = loader.AllCategories()
		} else {
			categories = h.st.AllCategories(c)
		}
		data["Categories"] = categories
	}
	if needs("Tags") {
		if loader != nil {
			tags = loader.AllTags()
		} else {
			tags = h.st.AllTags(c)
		}
		data["Tags"] = tags
	}

	// 缓存主题名，避免 renderWidgets 中每个 widget 都查一次
	var themeName string
	if loader != nil {
		themeName = h.currentThemeNameFromLoader(loader)
	} else {
		themeName = h.currentThemeName(c)
	}

	// 渲染 widgets，传入预查询数据避免重复查询。
	widgets := h.renderWidgets(c, s, pageCfg, theme.WidgetSettings{
		RecentPosts:        recentPosts,
		RecentCommentItems: recentCommentItems,
		SayingCommentItems: sayingCommentItems,
		ArchiveMonths:      archiveMonths,
		Categories:         categories,
		Tags:               tags,
		ThemeName:          themeName,
	})
	data["Widgets"] = widgets

	// 管理员且设置开启时，注入 SQL 详情供模板 footer 输出
	if currentUser != nil && currentUser.Role == model.RoleAdmin && s.ShowSQLDetails {
		data["SQLDetails"] = &store.LazySQLDetails{Ctx: c.Request.Context()}
	}
	return i18n.InjectDomain(c, data, themeName)
}

// renderWidgets 根据 pageCfg 渲染 widget HTML 片段列表。
// preData 包含 base() 中已查询的数据，避免 widget Data() 重复查询。
func (h *Public) renderWidgets(c *gin.Context, s publicSettings, pageCfg *theme.PageConfig, preData theme.WidgetSettings) []template.HTML {
	if pageCfg == nil || len(pageCfg.Widgets) == 0 {
		return nil
	}
	currentUser := h.currentUser(c)
	settings := theme.WidgetSettings{
		SayingPostID:       s.SayingPostID,
		DefaultAvatar:      s.DefaultAvatar,
		CurrentUserID:      currentUserID(c),
		CurrentUserName:    displayUserName(currentUser),
		RegistrationOpen:   s.RegistrationOpen,
		Keyword:            c.Query("q"),
		CSRFToken:          "",
		RecentPosts:        preData.RecentPosts,
		RecentCommentItems: preData.RecentCommentItems,
		SayingCommentItems: preData.SayingCommentItems,
		ArchiveMonths:      preData.ArchiveMonths,
		Categories:         preData.Categories,
		Tags:               preData.Tags,
		ThemeName:          preData.ThemeName,
	}
	if uid := currentUserID(c); uid != 0 {
		if token, err := middleware.EnsureCSRFToken(c); err == nil {
			settings.CSRFToken = token
		}
	}
	var widgets []template.HTML
	for _, name := range pageCfg.Widgets {
		w := theme.Get(name)
		if w == nil {
			continue
		}
		html, err := h.renderWidget(c, w, settings)
		if err != nil {
			if h.log != nil {
				h.log.Error("render widget", "name", name, "error", err)
			}
			continue
		}
		if html != "" {
			widgets = append(widgets, html)
		}
	}
	return widgets
}

func (h *Public) renderWidget(c *gin.Context, w theme.Widget, settings theme.WidgetSettings) (template.HTML, error) {
	if h.renderer == nil {
		return "", nil
	}
	tpl := h.renderer.Template()
	name := theme.WidgetTemplateName(w.Name())
	if tpl == nil || tpl.Lookup(name) == nil {
		return "", nil
	}
	data, err := w.Data(c, h.st, settings)
	if err != nil || data == nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, name, widgetTemplateData(c, data, settings.ThemeName)); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func widgetTemplateData(c *gin.Context, data any, domain string) gin.H {
	result := gin.H{}
	switch v := data.(type) {
	case gin.H:
		for key, value := range v {
			result[key] = value
		}
	case map[string]any:
		for key, value := range v {
			result[key] = value
		}
	default:
		rv := reflect.ValueOf(data)
		if rv.IsValid() && rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
		}
		if rv.IsValid() && rv.Kind() == reflect.Struct {
			rt := rv.Type()
			for i := 0; i < rv.NumField(); i++ {
				field := rt.Field(i)
				if field.PkgPath != "" {
					continue
				}
				result[field.Name] = rv.Field(i).Interface()
			}
		} else {
			result["Data"] = data
		}
	}
	return i18n.InjectDomain(c, result, domain)
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

func (h *Public) mailEnabled() bool {
	return smtpConfigFromStore(context.Background(), h.st).Configured()
}

// pageConfig 从当前主题获取指定页面的配置，无主题时返回 nil。
func (h *Public) pageConfig(c *gin.Context, pageName string) *theme.PageConfig {
	return h.pageConfigFromTheme(h.currentTheme(c), pageName)
}

func (h *Public) pageConfigFromTheme(t *theme.Theme, pageName string) *theme.PageConfig {
	if t == nil {
		return nil
	}
	cfg, ok := t.Pages[pageName]
	if !ok {
		return nil
	}
	return &cfg
}

func (h *Public) currentTheme(c *gin.Context) *theme.Theme {
	if h.themeManager == nil {
		return nil
	}
	return h.themeManager.Current(c)
}

// currentThemeFromLoader 从 DataLoader 内存中读取 current_theme，不查 DB。
func (h *Public) currentThemeFromLoader(loader *store.DataLoader) *theme.Theme {
	if h.themeManager == nil {
		return nil
	}
	name := loader.GetSetting("current_theme")
	if name == "" {
		name = "default"
	}
	return h.themeManager.Get(name)
}

func (h *Public) currentThemeName(c *gin.Context) string {
	if t := h.currentTheme(c); t != nil {
		return t.Name
	}
	return ""
}

// currentThemeNameFromLoader 从 DataLoader 内存中读取主题名，不查 DB。
func (h *Public) currentThemeNameFromLoader(loader *store.DataLoader) string {
	if t := h.currentThemeFromLoader(loader); t != nil {
		return t.Name
	}
	return ""
}

func (h *Public) loadSettings(ctx context.Context) publicSettings {
	settings, err := h.st.GetSettings(ctx, consts.SettingsSiteName,
		consts.SettingsSiteDesc,
		consts.SettingsPageSize,
		consts.SettingsFeedSize,
		consts.SettingsSayingPageID,
		consts.SettingsDefaultAvatar,
		consts.SettingsRegistrationOpen,
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
		consts.SettingsPageSize,
		consts.SettingsFeedSize,
		consts.SettingsSayingPageID,
		consts.SettingsDefaultAvatar,
		consts.SettingsRegistrationOpen,
		consts.SettingsShowSQLDetails,
	)
	return h.buildSettings(settings)
}

func (h *Public) buildSettings(settings map[string]string) publicSettings {
	return publicSettings{
		SiteName:         util.FirstNonEmpty(settings[consts.SettingsSiteName], consts.SettingsSiteNameDefault),
		SiteDescription:  settings[consts.SettingsSiteDesc],
		PostPermalink:    permalink.CurrentPostPattern(),
		CategoryPrefix:   permalink.CurrentCategoryPrefix(),
		TagPrefix:        permalink.CurrentTagPrefix(),
		PageSize:         positiveIntSetting(settings[consts.SettingsPageSize], defaultPublicPageSize),
		FeedSize:         positiveIntSetting(settings[consts.SettingsFeedSize], defaultFeedSize),
		SayingPostID:     uint(positiveIntSetting(settings[consts.SettingsSayingPageID], consts.SettingsSayingPageIDDefault)),
		DefaultAvatar:    util.NormalizeDefaultAvatar(settings[consts.SettingsDefaultAvatar]),
		RegistrationOpen: settings[consts.SettingsRegistrationOpen] == "true",
		ShowSQLDetails:   settings[consts.SettingsShowSQLDetails] == "true",
	}
}

// DynamicOrLegacy 是前台兜底路由：页面、文章固定链接与旧链接兼容都在这里收口。
func (h *Public) DynamicOrLegacy(c *gin.Context) {
	path := c.Request.URL.EscapedPath()

	// 全量预加载，后续所有数据从内存读取
	loader, err := h.st.LoadAllCached(c)
	if err != nil {
		h.serverError(c, err)
		return
	}
	syncPostPermalinkFromLoader(loader)

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
	if h.LegacyQueryRedirect(c) {
		return
	}
	h.notFound(c)
}

// currentUser 返回当前登录用户(未登录为 nil)。
func (h *Public) currentUser(c *gin.Context) *model.User {
	return currentUserByStore(c, h.st, c)
}

func (h *Public) Index(c *gin.Context) {
	page := atoiDefault(c.Query("page"), 1)

	// 全量预加载，后续所有数据从内存读取
	loader, err := h.st.LoadAllCached(c)
	if err != nil {
		h.serverError(c, err)
		return
	}
	syncPostPermalinkFromLoader(loader)

	s := h.loadSettingsFromLoader(loader)
	res := loader.ListPosts(page, s.PageSize, "", "")

	// 从 loader 读主题名，避免 current_theme 查 DB
	t := h.currentThemeFromLoader(loader)
	pageCfg := h.pageConfigFromTheme(t, "index")
	data := h.base(c, s.SiteName, "", s, pageCfg, loader)
	data["List"] = res
	data["Pager"] = pager(res, "/")
	c.HTML(http.StatusOK, "index.gohtml", data)
}

// Search 全文搜索(标题/正文 LIKE)。
func (h *Public) Search(c *gin.Context) {
	kw := strings.TrimSpace(c.Query("q"))
	page := atoiDefault(c.Query("page"), 1)
	syncPostPermalink(c, h.st)
	s := h.loadSettings(c)
	var res *store.ListPostsResult
	var err error
	if kw == "" {
		res = &store.ListPostsResult{Page: 1}
	} else {
		res, err = h.st.SearchPosts(c, kw, page, s.PageSize)
		if err != nil {
			h.serverError(c, err)
			return
		}
	}
	tr := i18n.Get(c)
	data := h.base(c, tr.T("搜索:%s", kw), "", s, h.pageConfig(c, "list"), nil)
	data["Heading"] = tr.T("搜索「%s」", kw)
	data["Keyword"] = kw
	data["List"] = res
	data["Pager"] = pager(res, "/search?q="+url.QueryEscape(kw))
	c.HTML(http.StatusOK, "list.gohtml", data)
}

// Post 文章详情（按当前固定链接规则解析）。
func (h *Public) Post(c *gin.Context) {
	path := c.Request.URL.EscapedPath()
	match, ok := permalink.ParsePostPath(path)
	if !ok {
		h.notFound(c)
		return
	}
	if !h.renderResolvedPost(c, path, match) {
		h.notFound(c)
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

// Page 处理 /{slug} 页面以及若干特殊页面(归档)。
func (h *Public) Page(c *gin.Context) {
	slug := strings.Trim(c.Param("slug"), "/")
	if slug == "" {
		h.notFound(c)
		return
	}

	// 全量预加载
	loader, err := h.st.LoadAllCached(c)
	if err != nil {
		h.serverError(c, err)
		return
	}
	syncPostPermalinkFromLoader(loader)

	if slug == "archive" {
		h.renderArchive(c, h.specialPage(c, "archive", "归档"), loader)
		return
	}
	p := loader.GetPageBySlug(slug)
	if p == nil {
		h.notFound(c)
		return
	}
	if p.Status == model.StatusPublished {
		if err := h.st.IncrementViews(c, p.ID); err != nil && h.log != nil {
			h.log.Error("increment page views", "error", err, "post_id", p.ID)
		}
		p.Views++
	}

	commentPage := atoiDefault(c.Query("cpage"), 1)
	var comments *store.CommentPageResult
	uid := currentUserID(c)
	pendingIDs := pendingCommentIDs(c)
	if uid == 0 && len(pendingIDs) == 0 {
		comments = loader.CommentPage(p.ID, commentPage, commentPageSize)
	} else {
		var err error
		comments, err = h.st.VisibleCommentsPageForViewer(c, p.ID, commentPage, commentPageSize, uid, pendingIDs)
		if err != nil {
			h.serverError(c, err)
			return
		}
	}
	s := h.loadSettingsFromLoader(loader)
	t := h.currentThemeFromLoader(loader)
	pageCfg := h.pageConfigFromTheme(t, "page")
	data := h.base(c, p.Title, p.Excerpt, s, pageCfg, loader)
	data["Post"] = p
	data["Comments"] = comments.Comments
	data["CommentPager"] = gin.H{"Page": comments.Page, "Pages": comments.Pages, "BaseURL": permalink.Page(p), "Sep": "?"}
	data["CommentCount"] = comments.TotalComments
	data["CommentOpen"] = p.CommentStatus != "closed"
	data["RememberedCommenter"] = rememberedCommenter(c)
	if c.Query("ajax") == "comments" {
		c.HTML(http.StatusOK, "comments_fragment.gohtml", data)
		return
	}
	c.HTML(http.StatusOK, "page.gohtml", data)
}

func (h *Public) pageExists(ctx context.Context, slug string) bool {
	if slug == "archive" {
		return true
	}
	if h.st == nil {
		return false
	}
	_, err := h.st.GetPageBySlug(ctx, slug)
	return err == nil
}

func (h *Public) pageExistsFromLoader(loader *store.DataLoader, slug string) bool {
	if slug == "archive" {
		return true
	}
	return loader.GetPageBySlug(slug) != nil
}

func (h *Public) pageWithLoader(c *gin.Context, loader *store.DataLoader) {
	slug := strings.Trim(c.Param("slug"), "/")
	if slug == "" {
		h.notFound(c)
		return
	}
	if slug == "archive" {
		h.renderArchive(c, h.specialPageFromLoader(loader, "archive", "归档"), loader)
		return
	}
	p := loader.GetPageBySlug(slug)
	if p == nil {
		h.notFound(c)
		return
	}
	if p.Status == model.StatusPublished {
		if err := h.st.IncrementViews(c, p.ID); err != nil && h.log != nil {
			h.log.Error("increment page views", "error", err, "post_id", p.ID)
		}
		p.Views++
	}

	commentPage := atoiDefault(c.Query("cpage"), 1)
	var comments *store.CommentPageResult
	uid := currentUserID(c)
	pendingIDs := pendingCommentIDs(c)
	if uid == 0 && len(pendingIDs) == 0 {
		comments = loader.CommentPage(p.ID, commentPage, commentPageSize)
	} else {
		var err error
		comments, err = h.st.VisibleCommentsPageForViewer(c, p.ID, commentPage, commentPageSize, uid, pendingIDs)
		if err != nil {
			h.serverError(c, err)
			return
		}
	}
	s := h.loadSettingsFromLoader(loader)
	t := h.currentThemeFromLoader(loader)
	pageCfg := h.pageConfigFromTheme(t, "page")
	data := h.base(c, p.Title, p.Excerpt, s, pageCfg, loader)
	data["Post"] = p
	data["Comments"] = comments.Comments
	data["CommentPager"] = gin.H{"Page": comments.Page, "Pages": comments.Pages, "BaseURL": permalink.Page(p), "Sep": "?"}
	data["CommentCount"] = comments.TotalComments
	data["CommentOpen"] = p.CommentStatus != "closed"
	data["RememberedCommenter"] = rememberedCommenter(c)
	if c.Query("ajax") == "comments" {
		c.HTML(http.StatusOK, "comments_fragment.gohtml", data)
		return
	}
	c.HTML(http.StatusOK, "page.gohtml", data)
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
	t := h.currentThemeFromLoader(loader)
	pageCfg := h.pageConfigFromTheme(t, "list")
	data := h.base(c, tr.T("分类:%s", displaySlug), "", s, pageCfg, loader)
	data["Heading"] = tr.T("分类:%s", displaySlug)
	data["List"] = res
	data["Pager"] = pager(res, permalink.Category(slug))
	c.HTML(http.StatusOK, "list.gohtml", data)
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
	t := h.currentThemeFromLoader(loader)
	pageCfg := h.pageConfigFromTheme(t, "list")
	data := h.base(c, tr.T("标签:%s", displaySlug), "", s, pageCfg, loader)
	data["Heading"] = tr.T("标签:%s", displaySlug)
	data["List"] = res
	data["Pager"] = pager(res, permalink.Tag(slug))
	c.HTML(http.StatusOK, "list.gohtml", data)
}

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
			h.log.Error("increment post views", "error", err, "post_id", p.ID)
		}
		p.Views++
	}
	// 从内存填充关联数据
	loader.FillPost(p)

	commentPage := atoiDefault(c.Query("cpage"), 1)
	var comments *store.CommentPageResult
	uid := currentUserID(c)
	pendingIDs := pendingCommentIDs(c)
	if uid == 0 && len(pendingIDs) == 0 {
		comments = loader.CommentPage(p.ID, commentPage, commentPageSize)
	} else {
		var cerr error
		comments, cerr = h.st.VisibleCommentsPageForViewer(c, p.ID, commentPage, commentPageSize, uid, pendingIDs)
		if cerr != nil {
			h.serverError(c, cerr)
			return true
		}
	}
	s := h.loadSettingsFromLoader(loader)
	t := h.currentThemeFromLoader(loader)
	pageCfg := h.pageConfigFromTheme(t, "post")
	data := h.base(c, p.Title, p.Excerpt, s, pageCfg, loader)
	data["Post"] = p
	data["Comments"] = comments.Comments
	data["CommentPager"] = gin.H{"Page": comments.Page, "Pages": comments.Pages, "BaseURL": permalink.Post(p), "Sep": "?"}
	data["CommentCount"] = comments.TotalComments
	data["CommentOpen"] = p.CommentStatus != "closed"
	data["RememberedCommenter"] = rememberedCommenter(c)
	if c.Query("ajax") == "comments" {
		c.HTML(http.StatusOK, "comments_fragment.gohtml", data)
		return true
	}
	c.HTML(http.StatusOK, "post.gohtml", data)
	return true
}

func (h *Public) specialPageFromLoader(loader *store.DataLoader, slug, fallbackTitle string) *model.Post {
	if p := loader.GetPageBySlug(slug); p != nil {
		return p
	}
	tr := i18n.Get(nil) // fallback: no context available
	return &model.Post{Title: tr.T(fallbackTitle), Slug: slug, PostType: model.PostTypePage}
}

func (h *Public) renderResolvedPost(c *gin.Context, path string, match *permalink.PostPathMatch) bool {
	// 全量预加载
	loader, err := h.st.LoadAllCached(c)
	if err != nil {
		h.serverError(c, err)
		return true
	}
	syncPostPermalinkFromLoader(loader)

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
			h.log.Error("increment post views", "error", err, "post_id", p.ID)
		}
		p.Views++
	}
	// 从内存填充关联数据
	loader.FillPost(p)

	commentPage := atoiDefault(c.Query("cpage"), 1)
	var comments *store.CommentPageResult
	uid := currentUserID(c)
	pendingIDs := pendingCommentIDs(c)
	if uid == 0 && len(pendingIDs) == 0 {
		// 匿名访客：从内存取评论，不查 DB
		comments = loader.CommentPage(p.ID, commentPage, commentPageSize)
	} else {
		var err error
		comments, err = h.st.VisibleCommentsPageForViewer(c, p.ID, commentPage, commentPageSize, uid, pendingIDs)
		if err != nil {
			h.serverError(c, err)
			return true
		}
	}
	s := h.loadSettingsFromLoader(loader)
	t := h.currentThemeFromLoader(loader)
	pageCfg := h.pageConfigFromTheme(t, "post")
	data := h.base(c, p.Title, p.Excerpt, s, pageCfg, loader)
	data["Post"] = p
	data["IsDraft"] = p.Status == model.StatusDraft
	data["Comments"] = comments.Comments
	data["CommentPager"] = gin.H{"Page": comments.Page, "Pages": comments.Pages, "BaseURL": permalink.Post(p), "Sep": "?"}
	data["CommentCount"] = comments.TotalComments
	data["CommentOpen"] = p.CommentStatus != "closed"
	data["RememberedCommenter"] = rememberedCommenter(c)
	if prev := loader.PrevPost(p.PublishedAt); prev != nil {
		data["PrevPost"] = prev
	}
	if next := loader.NextPost(p.PublishedAt); next != nil {
		data["NextPost"] = next
	}
	if c.Query("ajax") == "comments" {
		c.HTML(http.StatusOK, "comments_fragment.gohtml", data)
		return true
	}
	c.HTML(http.StatusOK, "post.gohtml", data)
	return true
}

func (h *Public) specialPage(c *gin.Context, slug, fallbackTitle string) *model.Post {
	if h.st != nil {
		p, err := h.st.GetPageBySlug(c, slug)
		if err == nil {
			return p
		}
	}
	tr := i18n.Get(c)
	return &model.Post{Title: tr.T(fallbackTitle), Slug: slug, PostType: model.PostTypePage}
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
	t := h.currentThemeFromLoader(loader)
	pageCfg := h.pageConfigFromTheme(t, "archive")
	data := h.base(c, p.Title, "", s, pageCfg, loader)
	data["Post"] = p
	data["Groups"] = groups
	c.HTML(http.StatusOK, "archive.gohtml", data)
}

// Category 分类列表页。
func (h *Public) Category(c *gin.Context) {
	displaySlug := c.Param("slug")
	if displaySlug == "" {
		displaySlug = c.Query("slug")
	}
	slug := encodeTaxonomySlug(displaySlug)
	page := atoiDefault(c.Query("page"), 1)

	loader, err := h.st.LoadAllCached(c)
	if err != nil {
		h.serverError(c, err)
		return
	}
	syncPostPermalinkFromLoader(loader)

	s := h.loadSettingsFromLoader(loader)
	res := loader.ListPosts(page, s.PageSize, slug, "")
	tr := i18n.Get(c)
	t := h.currentThemeFromLoader(loader)
	pageCfg := h.pageConfigFromTheme(t, "list")
	data := h.base(c, tr.T("分类:%s", displaySlug), "", s, pageCfg, loader)
	data["Heading"] = tr.T("分类:%s", displaySlug)
	data["List"] = res
	data["Pager"] = pager(res, permalink.Category(slug))
	c.HTML(http.StatusOK, "list.gohtml", data)
}

// Tag 标签列表页。
func (h *Public) Tag(c *gin.Context) {
	displaySlug := c.Param("slug")
	if displaySlug == "" {
		displaySlug = c.Query("slug")
	}
	slug := encodeTaxonomySlug(displaySlug)
	page := atoiDefault(c.Query("page"), 1)

	loader, err := h.st.LoadAllCached(c)
	if err != nil {
		h.serverError(c, err)
		return
	}
	syncPostPermalinkFromLoader(loader)

	s := h.loadSettingsFromLoader(loader)
	res := loader.ListPosts(page, s.PageSize, "", slug)
	tr := i18n.Get(c)
	t := h.currentThemeFromLoader(loader)
	pageCfg := h.pageConfigFromTheme(t, "list")
	data := h.base(c, tr.T("标签:%s", displaySlug), "", s, pageCfg, loader)
	data["Heading"] = tr.T("标签:%s", displaySlug)
	data["List"] = res
	data["Pager"] = pager(res, permalink.Tag(slug))
	c.HTML(http.StatusOK, "list.gohtml", data)
}

// LegacyQueryRedirect 处理 /?p={id} 旧链接 301 到永久链接。
// 返回 true 表示已处理。
func (h *Public) LegacyQueryRedirect(c *gin.Context) bool {
	syncPostPermalink(c, h.st)
	pid := c.Query("p")
	if pid == "" {
		return false
	}
	id, err := strconv.ParseUint(pid, 10, 64)
	if err != nil {
		return false
	}
	p, err := h.st.PostMeta(c, uint(id))
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
	data := h.base(c, tr.T("页面不存在"), "", h.loadSettings(c), h.pageConfig(c, "error"), nil)
	data["Code"] = 404
	data["Message"] = tr.T("你访问的页面不存在或已被删除。")
	c.HTML(http.StatusNotFound, "error.gohtml", data)
}

func (h *Public) serverError(c *gin.Context, err error) {
	h.log.Error("handler error", slog.Any("error", err), slog.String("path", c.Request.URL.Path))
	tr := i18n.Get(c)
	data := h.base(c, tr.T("出错了"), "", h.loadSettings(c), h.pageConfig(c, "error"), nil)
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

func singleSegmentSlug(path string) (string, bool) {
	path = strings.Trim(path, "/")
	if path == "" || strings.ContainsRune(path, '/') {
		return "", false
	}
	// 排除文章永久链接格式（如 /20241.html），避免无用的 page 查询
	if strings.HasSuffix(path, ".html") {
		return "", false
	}
	return path, true
}

func encodeTaxonomySlug(slug string) string {
	return util.URLSlugify(slug)
}
