package hook

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/model"
	"github.com/youthlin/wenlog/internal/permalink"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/util"
)

// PostView 是扩展 API 暴露的文章/页面只读视图。
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

// PostURLFields 返回生成文章/页面永久链接所需的最小字段集。
// 返回类型保持匿名结构，避免 hook 包反向依赖 render 包。
func (p PostView) PostURLFields() struct {
	ID          uint
	Title       string
	Slug        string
	PostType    string
	PublishedAt time.Time
	ModifiedAt  time.Time
} {
	return struct {
		ID          uint
		Title       string
		Slug        string
		PostType    string
		PublishedAt time.Time
		ModifiedAt  time.Time
	}{
		ID:          p.ID,
		Title:       p.Title,
		Slug:        p.Slug,
		PostType:    p.PostType,
		PublishedAt: p.PublishedAt,
		ModifiedAt:  p.ModifiedAt,
	}
}

// CategoryView 是扩展 API 暴露的分类只读视图。
type CategoryView struct {
	ID          uint
	Name        string
	Slug        string
	Description string
	ParentID    uint
	PostCount   int64
}

// TagView 是扩展 API 暴露的标签只读视图。
type TagView struct {
	ID        uint
	Name      string
	Slug      string
	PostCount int64
}

// CommentView 是扩展 API 暴露的评论只读视图。
type CommentView struct {
	ID        uint
	PostID    uint
	ParentID  uint
	ReplyToID uint
	UserID    *uint
	Author    string
	Email     string
	URL       string
	IP        string
	Content   string
	// Status: approved / pending / spam / deleted。
	Status        string
	NotifyOnReply bool
	CreatedAt     time.Time
	ReplyToAuthor string
	CommenterRole string
}

// UserView 是扩展 API 暴露的用户只读视图。
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

// Posts 返回全部已发布文章（按发布时间倒序）。
func (api *API) Posts() []PostView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	posts := loader.PostsByType(model.PostTypePost)
	result := make([]PostView, 0, len(posts))
	for _, p := range posts {
		if v := toPostView(p, loader); v != nil {
			result = append(result, *v)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PublishedAt.After(result[j].PublishedAt) })
	return result
}

// Post 按 ID 返回文章或页面。
func (api *API) Post(postID any) *PostView {
	loader := api.loader()
	id := toUint(postID)
	if id == 0 || loader == nil {
		return nil
	}
	return toPostView(loader.Posts[id], loader)
}

// Pages 返回全部已发布页面。
func (api *API) Pages() []PostView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	pages := loader.PostsByType(model.PostTypePage)
	result := make([]PostView, 0, len(pages))
	for _, p := range pages {
		if v := toPostView(p, loader); v != nil {
			result = append(result, *v)
		}
	}
	return result
}

// PageBySlug 按 slug 获取页面。
func (api *API) PageBySlug(slug string) *PostView {
	loader := api.loader()
	if loader == nil || slug == "" {
		return nil
	}
	return toPostView(loader.GetPageBySlug(slug), loader)
}

// RecentPosts 返回最近 n 篇文章。
func (api *API) RecentPosts(n int) []PostView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	posts := loader.RecentPosts(n)
	result := make([]PostView, 0, len(posts))
	for i := range posts {
		if v := toPostView(&posts[i], loader); v != nil {
			result = append(result, *v)
		}
	}
	return result
}

// PostsByCategory 返回指定分类下的文章。
func (api *API) PostsByCategory(categorySlug string) []PostView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	res := loader.ListPosts(1, 10000, categorySlug, "")
	result := make([]PostView, 0, len(res.Posts))
	for i := range res.Posts {
		if v := toPostView(&res.Posts[i], loader); v != nil {
			result = append(result, *v)
		}
	}
	return result
}

// PostsByTag 返回指定标签下的文章。
func (api *API) PostsByTag(tagSlug string) []PostView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	res := loader.ListPosts(1, 10000, "", tagSlug)
	result := make([]PostView, 0, len(res.Posts))
	for i := range res.Posts {
		if v := toPostView(&res.Posts[i], loader); v != nil {
			result = append(result, *v)
		}
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
	loader := api.loader()
	if loader == nil {
		return nil
	}
	cats := loader.AllCategories()
	result := make([]CategoryView, 0, len(cats))
	for i := range cats {
		result = append(result, toCategoryView(&cats[i]))
	}
	return result
}

// Tags 返回全部标签。
func (api *API) Tags() []TagView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	tags := loader.AllTags()
	result := make([]TagView, 0, len(tags))
	for i := range tags {
		result = append(result, toTagView(&tags[i]))
	}
	return result
}

// CommentsByPost 返回指定文章的已批准评论。
func (api *API) CommentsByPost(postID any) []CommentView {
	loader := api.loader()
	id := toUint(postID)
	if loader == nil || id == 0 {
		return nil
	}
	commentIDs := loader.CommentIDsByPost(id)
	result := make([]CommentView, 0, len(commentIDs))
	for _, cid := range commentIDs {
		if c, ok := loader.Comments[cid]; ok {
			result = append(result, toCommentView(c))
		}
	}
	return result
}

// RecentComments 返回最近 n 条已批准评论。
func (api *API) RecentComments(n int) []CommentView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	comments := loader.RecentComments(n)
	result := make([]CommentView, 0, len(comments))
	for i := range comments {
		result = append(result, toCommentView(&comments[i]))
	}
	return result
}

// Users 返回全部用户。
func (api *API) Users() []UserView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	result := make([]UserView, 0, len(loader.Users))
	for _, u := range loader.Users {
		if v := toUserView(u); v != nil {
			result = append(result, *v)
		}
	}
	return result
}

// User 按 ID 返回用户。
func (api *API) User(userID any) *UserView {
	loader := api.loader()
	id := toUint(userID)
	if id == 0 || loader == nil {
		return nil
	}
	return toUserView(loader.Users[id])
}

// ArchiveMonths 返回归档月份统计。
func (api *API) ArchiveMonths() []ArchiveMonthView {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	months := loader.ArchiveMonths()
	result := make([]ArchiveMonthView, 0, len(months))
	for _, m := range months {
		result = append(result, ArchiveMonthView{Year: m.Year, Month: m.Month, Count: m.Count})
	}
	return result
}

// PostURL 生成文章永久链接。
func (api *API) PostURL(post any) string {
	p := postModel(post)
	if p == nil {
		return ""
	}
	return permalink.Post(viewPostModel(p))
}

// PageURL 生成页面永久链接。
func (api *API) PageURL(post any) string {
	p := postModel(post)
	if p == nil {
		return ""
	}
	return permalink.Page(viewPostModel(p))
}

func postModel(v any) *PostView {
	switch p := v.(type) {
	case *PostView:
		return p
	case PostView:
		return &p
	default:
		return nil
	}
}

func viewPostModel(p *PostView) *model.Post {
	if p == nil {
		return nil
	}
	return &model.Post{ID: p.ID, Title: p.Title, Slug: p.Slug, PostType: p.PostType, Status: p.Status, PublishedAt: p.PublishedAt, ModifiedAt: p.ModifiedAt}
}

func toPostView(p *model.Post, loader *store.DataLoader) *PostView {
	if p == nil {
		return nil
	}
	v := &PostView{
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
			if author := toUserView(u); author != nil {
				v.Author = *author
			}
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
	if c == nil {
		return CategoryView{}
	}
	return CategoryView{ID: c.ID, Name: c.Name, Slug: c.Slug, Description: c.Description, ParentID: c.ParentID, PostCount: c.PostCount}
}

func toTagView(t *model.Tag) TagView {
	if t == nil {
		return TagView{}
	}
	return TagView{ID: t.ID, Name: t.Name, Slug: t.Slug, PostCount: t.PostCount}
}

func toCommentView(c *model.Comment) CommentView {
	if c == nil {
		return CommentView{}
	}
	return CommentView{
		ID:            c.ID,
		PostID:        c.PostID,
		ParentID:      c.ParentID,
		ReplyToID:     c.ReplyToID,
		UserID:        c.UserID,
		Author:        c.Author,
		Email:         c.Email,
		URL:           c.URL,
		IP:            c.IP,
		Content:       c.Content,
		Status:        c.Status,
		NotifyOnReply: c.NotifyOnReply,
		CreatedAt:     c.CreatedAt,
		ReplyToAuthor: c.ReplyToAuthor,
		CommenterRole: c.CommenterRole,
	}
}

func toUserView(u *model.User) *UserView {
	if u == nil {
		return nil
	}
	return &UserView{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, Website: u.Website, Role: u.Role}
}

func toUint(v any) uint {
	switch n := v.(type) {
	case uint:
		return n
	case uint64:
		return uint(n)
	case uint32:
		return uint(n)
	case int:
		if n > 0 {
			return uint(n)
		}
	case int64:
		if n > 0 {
			return uint(n)
		}
	case int32:
		if n > 0 {
			return uint(n)
		}
	case string:
		parsed, _ := strconv.ParseUint(strings.TrimSpace(n), 10, 64)
		return uint(parsed)
	}
	return 0
}

// CommentURL 生成评论锚点链接，评论不在第一页时自动拼接 cpage 参数。
func (api *API) CommentURL(post any, comment any) string {
	base := ""
	var pv *PostView
	if pv = postModel(post); pv != nil {
		if pv.PostType == model.PostTypePage {
			base = api.PageURL(pv)
		} else {
			base = api.PostURL(pv)
		}
	}
	cid := commentID(comment)
	if pv != nil {
		if loader := api.loader(); loader != nil {
			if page := loader.CommentPageForID(pv.ID, cid, 20); page > 1 {
				return base + "?cpage=" + strconv.Itoa(page) + "#comment-" + strconv.Itoa(int(cid))
			}
		}
	}
	return base + "#comment-" + strconv.Itoa(int(cid))
}

// CategoryURL 生成分类永久链接。
func (api *API) CategoryURL(slug string) string { return permalink.Category(slug) }

// TagURL 生成标签永久链接。
func (api *API) TagURL(slug string) string { return permalink.Tag(slug) }

func commentID(v any) uint {
	switch c := v.(type) {
	case CommentView:
		return c.ID
	case *CommentView:
		if c != nil {
			return c.ID
		}
	}
	return toUint(v)
}

// Snippet 截取一段文本摘要。
func (api *API) Snippet(content any, n int) string {
	if n <= 0 {
		n = 36
	}
	runes := []rune(fmt.Sprint(content))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n]) + "…"
}

// AvatarURL 由邮箱生成 cravatar(国内镜像)头像 URL。
func (api *API) AvatarURL(email, defaultAvatar string) string {
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email))))
	hash := hex.EncodeToString(sum[:])
	return "https://cn.cravatar.com/avatar/" + hash + "?s=" + strconv.Itoa(consts.AvatarSizeSmall) + "&d=" + util.NormalizeDefaultAvatar(defaultAvatar)
}
