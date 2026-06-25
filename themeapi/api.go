// Package themeapi 提供宿主暴露给主题脚本的 API 与只读视图模型。
package themeapi

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/store"
)

// Api 是当前注入到主题脚本运行环境中的宿主 API 实例。
var Api *API

// PostView 是文章的只读视图。
type PostView struct {
	ID           uint
	Title        string
	Slug         string
	Excerpt      string
	Content      string
	AuthorID     uint
	Status       string
	PostType     string
	Views        int64
	MenuOrder    int
	PublishedAt  time.Time
	ModifiedAt   time.Time
	CommentCount int64
	Author       UserView
	Categories   []CategoryView
	Tags         []TagView
}

// CategoryView 是分类的只读视图。
type CategoryView struct {
	ID          uint
	Name        string
	Slug        string
	Description string
	ParentID    uint
	PostCount   int64
}

// TagView 是标签的只读视图。
type TagView struct {
	ID        uint
	Name      string
	Slug      string
	PostCount int64
}

// CommentView 是评论的只读视图。
type CommentView struct {
	ID        uint
	PostID    uint
	ParentID  uint
	Author    string
	Content   string
	Status    string
	CreatedAt time.Time
}

// UserView 是用户的只读视图。
type UserView struct {
	ID          uint
	Username    string
	DisplayName string
	Email       string
	Website     string
	Role        string
}

// ArchiveMonthView 是归档月份的只读视图。
type ArchiveMonthView struct {
	Year  int
	Month int
	Count int64
}

// SayingItem 是博主动态组件的一条评论项。
type SayingItem struct {
	CommentURL  string
	AuthorURL   string
	Snippet     string
	AuthorName  string
	AuthorEmail string
}

// SelectOpt 是 select 类型选项的一个可选项。
type SelectOpt struct {
	Value string `yaml:"value" json:"value"`
	Label string `yaml:"label" json:"label"`
}

// OptionDecl 描述主题声明的一个可配置选项。
type OptionDecl struct {
	ID          string      `yaml:"id" json:"id"`
	Type        string      `yaml:"type" json:"type"`
	Label       string      `yaml:"label" json:"label"`
	Description string      `yaml:"description" json:"description"`
	Default     string      `yaml:"default" json:"default"`
	Min         *float64    `yaml:"min" json:"min,omitempty"`
	Max         *float64    `yaml:"max" json:"max,omitempty"`
	Options     []SelectOpt `yaml:"options" json:"options,omitempty"`
}

// Func 是 functions.goyaegi 中注册的主题函数。
type Func func(args map[string]any) any

// API 是暴露给主题脚本的只读数据视图。
type API struct {
	loader       *store.DataLoader
	funcs        map[string]Func
	themeOptions []OptionDecl
}

// New 创建 ThemeAPI 实例。
func New(loader *store.DataLoader) *API {
	return &API{loader: loader, funcs: make(map[string]Func)}
}

// SetLoader 设置当前模板渲染请求的 DataLoader。
func (api *API) SetLoader(loader *store.DataLoader) { api.loader = loader }

// RegisterFunc 注册一个命名主题函数。
func (api *API) RegisterFunc(name string, fn Func) { api.funcs[name] = fn }

// GetFunc 获取已注册的主题函数。
func (api *API) GetFunc(name string) Func { return api.funcs[name] }

// FuncNames 返回所有已注册主题函数名称。
func (api *API) FuncNames() []string {
	names := make([]string, 0, len(api.funcs))
	for name := range api.funcs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func toPostView(p *model.Post, loader *store.DataLoader) PostView {
	if p == nil {
		return PostView{}
	}
	v := PostView{
		ID:           p.ID,
		Title:        p.Title,
		Slug:         p.Slug,
		Excerpt:      p.Excerpt,
		Content:      p.Content,
		AuthorID:     p.AuthorID,
		Status:       p.Status,
		PostType:     p.PostType,
		Views:        p.Views,
		MenuOrder:    p.MenuOrder,
		PublishedAt:  p.PublishedAt,
		ModifiedAt:   p.ModifiedAt,
		CommentCount: p.CommentCount,
	}
	if loader != nil {
		if u, ok := loader.Users[p.AuthorID]; ok {
			v.Author = toUserView(u)
		}
		for _, cid := range loader.PostCategoryIDs(p.ID) {
			if c, ok := loader.Categories[cid]; ok {
				v.Categories = append(v.Categories, toCategoryView(c))
			}
		}
		for _, tid := range loader.PostTagIDs(p.ID) {
			if t, ok := loader.Tags[tid]; ok {
				v.Tags = append(v.Tags, toTagView(t))
			}
		}
	}
	return v
}

func toCategoryView(c *model.Category) CategoryView {
	return CategoryView{ID: c.ID, Name: c.Name, Slug: c.Slug, Description: c.Description, ParentID: c.ParentID, PostCount: c.PostCount}
}

func toTagView(t *model.Tag) TagView {
	return TagView{ID: t.ID, Name: t.Name, Slug: t.Slug, PostCount: t.PostCount}
}

func toCommentView(c *model.Comment) CommentView {
	return CommentView{ID: c.ID, PostID: c.PostID, ParentID: c.ParentID, Author: c.Author, Content: c.Content, Status: c.Status, CreatedAt: c.CreatedAt}
}

func toUserView(u *model.User) UserView {
	return UserView{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, Website: u.Website, Role: u.Role}
}

// Posts 返回全部已发布文章（按发布时间倒序）。
func (api *API) Posts() []PostView {
	posts := api.loader.PostsByType("post")
	result := make([]PostView, 0, len(posts))
	for _, p := range posts {
		result = append(result, toPostView(p, api.loader))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PublishedAt.After(result[j].PublishedAt) })
	return result
}

// Post 按 ID 获取单篇文章。
func (api *API) Post(id uint) *PostView {
	p := api.loader.Posts[id]
	if p == nil || p.PostType != model.PostTypePost {
		return nil
	}
	v := toPostView(p, api.loader)
	return &v
}

// Pages 返回全部已发布页面。
func (api *API) Pages() []PostView {
	pages := api.loader.PostsByType(model.PostTypePage)
	result := make([]PostView, 0, len(pages))
	for _, p := range pages {
		result = append(result, toPostView(p, api.loader))
	}
	return result
}

// PageBySlug 按 slug 获取页面。
func (api *API) PageBySlug(slug string) *PostView {
	p := api.loader.GetPageBySlug(slug)
	if p == nil {
		return nil
	}
	v := toPostView(p, api.loader)
	return &v
}

// RecentPosts 返回最近 n 篇文章。
func (api *API) RecentPosts(n int) []PostView {
	posts := api.loader.RecentPosts(n)
	result := make([]PostView, 0, len(posts))
	for i := range posts {
		result = append(result, toPostView(&posts[i], api.loader))
	}
	return result
}

// PostsByCategory 返回指定分类下的文章。
func (api *API) PostsByCategory(categorySlug string) []PostView {
	res := api.loader.ListPosts(1, 10000, categorySlug, "")
	result := make([]PostView, 0, len(res.Posts))
	for i := range res.Posts {
		result = append(result, toPostView(&res.Posts[i], api.loader))
	}
	return result
}

// PostsByTag 返回指定标签下的文章。
func (api *API) PostsByTag(tagSlug string) []PostView {
	res := api.loader.ListPosts(1, 10000, "", tagSlug)
	result := make([]PostView, 0, len(res.Posts))
	for i := range res.Posts {
		result = append(result, toPostView(&res.Posts[i], api.loader))
	}
	return result
}

// PostsByYear 返回指定年份的文章。
func (api *API) PostsByYear(year int) []PostView {
	all := api.Posts()
	result := make([]PostView, 0)
	for _, p := range all {
		if p.PublishedAt.Year() == year {
			result = append(result, p)
		}
	}
	return result
}

// PostsByYearMonth 返回指定年月的文章。
func (api *API) PostsByYearMonth(year, month int) []PostView {
	all := api.Posts()
	result := make([]PostView, 0)
	for _, p := range all {
		if p.PublishedAt.Year() == year && int(p.PublishedAt.Month()) == month {
			result = append(result, p)
		}
	}
	return result
}

// Categories 返回全部分类。
func (api *API) Categories() []CategoryView {
	cats := api.loader.AllCategories()
	result := make([]CategoryView, 0, len(cats))
	for i := range cats {
		result = append(result, toCategoryView(&cats[i]))
	}
	return result
}

// Tags 返回全部标签。
func (api *API) Tags() []TagView {
	tags := api.loader.AllTags()
	result := make([]TagView, 0, len(tags))
	for i := range tags {
		result = append(result, toTagView(&tags[i]))
	}
	return result
}

// CommentsByPost 返回指定文章的已批准评论。
func (api *API) CommentsByPost(postID uint) []CommentView {
	commentIDs := api.loader.CommentIDsByPost(postID)
	result := make([]CommentView, 0, len(commentIDs))
	for _, cid := range commentIDs {
		if c, ok := api.loader.Comments[cid]; ok {
			result = append(result, toCommentView(c))
		}
	}
	return result
}

// RecentComments 返回最近 n 条已批准评论。
func (api *API) RecentComments(n int) []CommentView {
	comments := api.loader.RecentComments(n)
	result := make([]CommentView, 0, len(comments))
	for i := range comments {
		result = append(result, toCommentView(&comments[i]))
	}
	return result
}

// Users 返回全部用户。
func (api *API) Users() []UserView {
	result := make([]UserView, 0, len(api.loader.Users))
	for _, u := range api.loader.Users {
		result = append(result, toUserView(u))
	}
	return result
}

// User 按 ID 获取用户。
func (api *API) User(id uint) *UserView {
	u, ok := api.loader.Users[id]
	if !ok {
		return nil
	}
	v := toUserView(u)
	return &v
}

// Setting 读取设置项。
func (api *API) Setting(key string) string { return api.loader.GetSetting(key) }

// SetThemeOptions 设置主题声明的选项列表（含默认值），供 GetOption 回退使用。
func (api *API) SetThemeOptions(opts []OptionDecl) { api.themeOptions = opts }

// GetOption 读取主题选项，未配置时回退到 theme.yaml 中的默认值。
func (api *API) GetOption(themeName, optionID string) string {
	return GetOptionByID(func(key string) (string, error) { return api.loader.GetSetting(key), nil }, themeName, api.themeOptions, optionID)
}

// Settings 批量读取设置项。
func (api *API) Settings(keys ...string) map[string]string { return api.loader.GetSettings(keys...) }

// ArchiveMonths 返回归档月份统计。
func (api *API) ArchiveMonths() []ArchiveMonthView {
	months := api.loader.ArchiveMonths()
	result := make([]ArchiveMonthView, 0, len(months))
	for _, m := range months {
		result = append(result, ArchiveMonthView{Year: m.Year, Month: m.Month, Count: m.Count})
	}
	return result
}

// PostURL 生成文章永久链接。
func (api *API) PostURL(p PostView) string {
	mp := &model.Post{ID: p.ID, Title: p.Title, Slug: p.Slug, PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: p.PublishedAt, ModifiedAt: p.ModifiedAt}
	return permalink.Post(mp)
}

// PageURL 生成页面永久链接。
func (api *API) PageURL(p PostView) string {
	mp := &model.Post{ID: p.ID, Title: p.Title, Slug: p.Slug, PostType: model.PostTypePage, Status: model.StatusPublished, PublishedAt: p.PublishedAt, ModifiedAt: p.ModifiedAt}
	return permalink.Page(mp)
}

// CategoryURL 生成分类永久链接。
func (api *API) CategoryURL(slug string) string { return permalink.Category(slug) }

// TagURL 生成标签永久链接。
func (api *API) TagURL(slug string) string { return permalink.Tag(slug) }

// SayingItems 返回指定文章下博主本人的评论，用于博主动态组件。
func (api *API) SayingItems(postID uint, n int) []SayingItem {
	if postID == 0 || api.loader == nil {
		return nil
	}
	comments := api.loader.SayingComments(postID, n)
	if len(comments) == 0 {
		return nil
	}
	p := api.loader.Posts[postID]
	if p == nil {
		return nil
	}
	author, ok := api.loader.Users[p.AuthorID]
	if !ok {
		return nil
	}
	authorName := author.DisplayName
	authorEmail := author.Email
	base := "/"
	if p.PostType == model.PostTypePage {
		base = permalink.Page(p)
	} else {
		base = permalink.Post(p)
	}
	var items []SayingItem
	for i := range comments {
		c := &comments[i]
		item := SayingItem{
			CommentURL:  base + "#comment-" + strconv.Itoa(int(c.ID)),
			Snippet:     commentSnippet(c.Content),
			AuthorName:  authorName,
			AuthorEmail: authorEmail,
		}
		if c.URL != "" {
			item.AuthorURL = c.URL
		}
		items = append(items, item)
	}
	return items
}

// OptionKey 返回 option 在 Setting 表中的 key。
func OptionKey(themeName, optionID string) string {
	return fmt.Sprintf("option_%s_%s", themeName, optionID)
}

// GetOption 从 Setting 表读取 option 值，未配置时返回 default 值。
func GetOption(getSetting func(key string) (string, error), themeName string, opt OptionDecl) string {
	key := OptionKey(themeName, opt.ID)
	val, err := getSetting(key)
	if err != nil || val == "" {
		return opt.Default
	}
	return val
}

// GetOptionByID 从选项声明列表中查找指定 option 并读取其配置值。
func GetOptionByID(getSetting func(key string) (string, error), themeName string, options []OptionDecl, optionID string) string {
	for _, opt := range options {
		if opt.ID == optionID {
			return GetOption(getSetting, themeName, opt)
		}
	}
	return ""
}

func commentSnippet(content string) string {
	runes := []rune(content)
	if len(runes) <= 36 {
		return content
	}
	return string(runes[:36]) + "…"
}
