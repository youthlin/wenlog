package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/store"
)

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
	cats := h.st.AllCategories(c)
	data["CurrentTermsSection"] = currentSection
	data["CurrentTermsPageURL"] = termsPageURL(currentSection)
	data["CategoriesPageURL"] = termsPageURL("category")
	data["TagsPageURL"] = termsPageURL("tag")
	data["CategoryParents"] = categoryParentNames(cats)
	data["CategoryParentOptions"] = cats
	data["CategoryCanDelete"] = categoryCanDelete(cats)
	data["CategoryForm"] = model.Category{}
	data["CategoryPostListURLs"] = categoryPostListURLs(cats)
	data["CategoryPublicURLs"] = categoryPublicURLs(cats)
	data["Keyword"] = keyword
	data["Page"] = page
	data["ListPageURL"] = termsListURL(currentSection, keyword, 1)

	if currentSection == "tag" {
		h.fillTagTermsData(c, data, tr, keyword, page)
	} else {
		h.fillCategoryTermsData(c, data, tr, keyword, page, cats)
	}
	return data
}

func (h *Admin) fillTagTermsData(c *gin.Context, data gin.H, tr *gettext.Translations, keyword string, page int) {
	allTags := h.st.AllTags(c)
	tags, total, err := h.st.AdminListTags(c, keyword, page, adminPageSize)
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
		fillPagination(data, keyword, page, pages)
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
}

func (h *Admin) fillCategoryTermsData(c *gin.Context, data gin.H, tr *gettext.Translations, keyword string, page int, cats []model.Category) {
	categories, total, err := h.st.AdminListCategories(c, keyword, page, adminPageSize)
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
		fillPagination(data, keyword, page, pages)
	}
	if c.Query("message") == "category-saved" {
		data["Notice"] = tr.T("分类已保存。")
	}
	if c.Query("message") == "category-deleted" {
		data["Notice"] = tr.T("分类已删除，文章已迁移到父分类或未分类。")
	}
	if c.Query("message") == "category-delete-blocked" {
		data["Error"] = tr.T("未分类是默认分类，不能删除。")
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
}

func fillPagination(data gin.H, keyword string, page, pages int) {
	if page > 1 {
		data["PrevPageURL"] = termsListURL(data["CurrentTermsSection"].(string), keyword, page-1)
	}
	if page < pages {
		data["NextPageURL"] = termsListURL(data["CurrentTermsSection"].(string), keyword, page+1)
	}
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
	if h.currentUserRole(c) != model.RoleAdmin {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
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
	slug := normalizeTaxonomySlug(f.Slug)
	if slug == "" {
		slug = normalizeTaxonomySlug(name)
	}
	if slug == "" {
		h.termsFormError(c, "category", tr.T("分类 slug 不能为空。"), model.Category{ID: f.ID, Name: name, Slug: f.Slug, Description: f.Description, ParentID: f.ParentID}, model.Tag{}, true, false)
		return
	}
	exists, err := h.st.CategorySlugExists(c, slug, f.ID)
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
	if err := h.st.SaveCategory(c, cat); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, termsRedirectURL("category", "category-saved"))
}

// DeleteCategory 删除分类。
func (h *Admin) DeleteCategory(c *gin.Context) {
	if h.currentUserRole(c) != model.RoleAdmin {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	id, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	if err := h.st.DeleteCategory(c, id); err != nil {
		if errors.Is(err, store.ErrCannotDeleteUncategorized) {
			c.Redirect(http.StatusSeeOther, termsRedirectURL("category", "category-delete-blocked"))
			return
		}
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
	if h.currentUserRole(c) != model.RoleAdmin {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
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
	slug := normalizeTaxonomySlug(f.Slug)
	if slug == "" {
		slug = normalizeTaxonomySlug(name)
	}
	if slug == "" {
		h.termsFormError(c, "tag", tr.T("标签 slug 不能为空。"), model.Category{}, model.Tag{ID: f.ID, Name: name, Slug: f.Slug}, false, true)
		return
	}
	exists, err := h.st.TagSlugExists(c, slug, f.ID)
	if err != nil {
		h.serverError(c, err)
		return
	}
	if exists {
		h.termsFormError(c, "tag", tr.T("标签 slug %q 已存在。", slug), model.Category{}, model.Tag{ID: f.ID, Name: name, Slug: slug}, false, true)
		return
	}
	if err := h.st.SaveTag(c, &model.Tag{ID: f.ID, Name: name, Slug: slug}); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, termsRedirectURL("tag", "tag-saved"))
}

// DeleteTag 删除标签。
func (h *Admin) DeleteTag(c *gin.Context) {
	if h.currentUserRole(c) != model.RoleAdmin {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	id, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	if err := h.st.DeleteTag(c, id); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, termsRedirectURL("tag", "tag-deleted"))
}

func categoryParentNames(categories []model.Category) map[uint]string {
	byID := make(map[uint]string, len(categories))
	for _, cat := range categories {
		byID[cat.ID] = cat.Name
	}
	return byID
}

func categoryCanDelete(categories []model.Category) map[uint]bool {
	out := make(map[uint]bool, len(categories))
	for _, cat := range categories {
		out[cat.ID] = cat.Slug != "uncategorized"
	}
	return out
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
