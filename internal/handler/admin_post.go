package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	renderx "github.com/youthlin/blog/internal/render"
)

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
			for _, cat := range h.st.AllCategories(c) {
				if cat.ID == categoryID {
					title = tr.T("文章管理 · 分类：%s", cat.Name)
					break
				}
			}
		}
		if tagID > 0 {
			for _, tag := range h.st.AllTags(c) {
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
	var authorID uint
	if h.currentUserRole(c) == model.RoleAuthor {
		authorID = currentUserID(c)
	}
	posts, total, err := h.st.AdminListPostsForAuthor(c, pt, page, adminPageSize, categoryID, tagID, keyword, authorID)
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
	data["AllCategories"] = h.st.AllCategories(c)
	data["AllTags"] = h.st.AllTags(c)
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

// EditPostForm 显示新建/编辑表单。id=0 或缺省为新建。
func (h *Admin) EditPostForm(c *gin.Context) {
	tr := i18n.Get(c)
	pt := c.DefaultQuery("type", model.PostTypePost)
	data := h.base(c, tr.T("内容管理"))
	data["PostType"] = pt
	allCategories := h.st.AllCategories(c)
	data["AllCategories"] = allCategories
	data["SelectedCats"] = map[uint]bool{}
	data["TagsCSV"] = ""
	if idStr := c.Param("id"); idStr != "" && idStr != "new" {
		id, err := parseUintParam(idStr)
		if err != nil {
			h.notFound(c)
			return
		}
		p, err := h.st.AdminGetPost(c, id)
		if err != nil {
			h.notFound(c)
			return
		}
		if !h.canManagePost(c, p) {
			c.String(http.StatusForbidden, "Forbidden")
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
	} else if pt == model.PostTypePost {
		data["SelectedCats"] = defaultCategorySelection(allCategories)
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
	allCategories := h.st.AllCategories(c)
	if f.PostType == model.PostTypePost && !hasSelectedCategory(f.CategoryIDs, allCategories) {
		h.postEditError(c, tr.T("请至少选择一个分类目录。"), f, allCategories, selectedCats(f.CategoryIDs))
		return
	}

	now := time.Now()
	p, err := h.resolvePostForSave(c, f, now)
	if err != nil {
		if err == errPostNotFound {
			h.notFound(c)
		} else if err == errPostForbidden {
			c.String(http.StatusForbidden, "Forbidden")
		} else {
			h.serverError(c, err)
		}
		return
	}

	p.Title = strings.TrimSpace(f.Title)
	p.Slug = strings.TrimSpace(f.Slug)
	if f.PostType == model.PostTypePage {
		if err := h.validateAndCheckPageSlug(c, tr, f, p, allCategories); err != nil {
			return
		}
	} else {
		if err := h.validateAndCheckPostSlug(c, tr, f, p, allCategories); err != nil {
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
	p.Content = renderx.RenderMarkdown(f.ContentMD)
	p.ContentFormat = model.FormatMarkdown

	// 仅文章关联分类/标签;页面不需要。
	if f.PostType == model.PostTypePost {
		tagNames := parseTags(f.Tags)
		if err := h.st.SavePostWithTerms(c, p, f.CategoryIDs, tagNames); err != nil {
			h.serverError(c, err)
			return
		}
	} else if err := h.st.SavePost(c, p); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/posts?type="+p.PostType)
}

var errPostNotFound = errors.New("post not found")
var errPostForbidden = errors.New("post forbidden")

func (h *Admin) resolvePostForSave(c *gin.Context, f postForm, now time.Time) (*model.Post, error) {
	if f.ID > 0 {
		existing, err := h.st.AdminGetPost(c, f.ID)
		if err != nil {
			return nil, errPostNotFound
		}
		if !h.canManagePost(c, existing) {
			return nil, errPostForbidden
		}
		return existing, nil
	}
	id, err := h.st.NextPostID(c)
	if err != nil {
		return nil, err
	}
	return &model.Post{ID: id, PublishedAt: now, AuthorID: currentUserID(c)}, nil
}

func (h *Admin) postEditError(c *gin.Context, errMsg string, f postForm, allCategories []model.Category, selectedCats map[uint]bool) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("内容管理"))
	data["Error"] = errMsg
	data["PostType"] = f.PostType
	data["AllCategories"] = allCategories
	data["SelectedCats"] = selectedCats
	data["TagsCSV"] = f.Tags
	data["Post"] = &model.Post{ID: f.ID, Title: strings.TrimSpace(f.Title), Slug: strings.TrimSpace(f.Slug), ContentMD: f.ContentMD, Excerpt: f.Excerpt, PostType: f.PostType, Status: f.Status, CommentStatus: f.CommentStatus, MenuOrder: f.MenuOrder}
	data["IsEdit"] = f.ID > 0
	c.HTML(http.StatusBadRequest, "admin_post_edit.gohtml", data)
}

func (h *Admin) validateAndCheckPageSlug(c *gin.Context, tr *gettext.Translations, f postForm, p *model.Post, allCategories []model.Category) error {
	if err := validatePageSlugT(tr.T, p.Slug); err != nil {
		h.postEditError(c, err.Error(), f, allCategories, map[uint]bool{})
		return err
	}
	exists, err := h.st.PageSlugExists(c, p.Slug, p.ID)
	if err != nil {
		h.serverError(c, err)
		return err
	}
	if exists {
		h.postEditError(c, tr.T("页面链接 /%s 已存在，请换一个 slug。", p.Slug), f, allCategories, map[uint]bool{})
		return errors.New("page slug exists")
	}
	return nil
}

func (h *Admin) validateAndCheckPostSlug(c *gin.Context, tr *gettext.Translations, f postForm, p *model.Post, allCategories []model.Category) error {
	p.Slug = normalizeTermSlug(p.Slug)
	if p.Slug == "" {
		p.Slug = normalizeTermSlug(p.Title)
	}
	if err := validatePostSlugT(tr.T, p.Slug); err != nil {
		h.postEditError(c, err.Error(), f, allCategories, selectedCats(f.CategoryIDs))
		return err
	}
	exists, err := h.st.PostSlugExists(c, p.Slug, p.ID)
	if err != nil {
		h.serverError(c, err)
		return err
	}
	if exists {
		h.postEditError(c, tr.T("文章 slug %q 已存在，请换一个。", p.Slug), f, allCategories, selectedCats(f.CategoryIDs))
		return errors.New("post slug exists")
	}
	return nil
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

func defaultCategorySelection(categories []model.Category) map[uint]bool {
	out := map[uint]bool{}
	if len(categories) == 0 {
		return out
	}
	for _, cat := range categories {
		if cat.Slug == "uncategorized" {
			out[cat.ID] = true
			return out
		}
	}
	out[categories[0].ID] = true
	return out
}

func hasSelectedCategory(selected []uint, categories []model.Category) bool {
	if len(selected) == 0 || len(categories) == 0 {
		return false
	}
	valid := make(map[uint]bool, len(categories))
	for _, cat := range categories {
		valid[cat.ID] = true
	}
	for _, id := range selected {
		if valid[id] {
			return true
		}
	}
	return false
}

// Preview 渲染 Markdown 为 HTML 片段(后台编辑预览,Ajax)。
func (h *Admin) Preview(c *gin.Context) {
	md := c.PostForm("content_md")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, renderx.RenderMarkdown(md))
}

// DeletePost 删除文章/页面。
func (h *Admin) DeletePost(c *gin.Context) {
	id, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	p, err := h.st.AdminGetPost(c, id)
	if err != nil {
		h.notFound(c)
		return
	}
	if !h.canManagePost(c, p) {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	if err := h.st.DeletePost(c, id); err != nil {
		h.serverError(c, err)
		return
	}
	if p.PostType == model.PostTypePage {
		c.Redirect(http.StatusSeeOther, "/admin/posts?type=page")
	} else {
		c.Redirect(http.StatusSeeOther, "/admin/posts")
	}
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
	syncPostPermalink(context.Background(), nil)
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
