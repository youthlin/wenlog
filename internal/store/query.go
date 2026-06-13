// Package store — 前台/后台查询方法。
package store

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"gorm.io/gorm"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
)

// CommentPageResult 是按顶层评论分页后的结果。
type CommentPageResult struct {
	Comments      []model.Comment
	TotalComments int64 // 当前访问者可见评论总数(含回复)
	TotalTop      int64 // 当前访问者可见顶层评论总数
	Page          int
	Pages         int
}

// CommentWidgetItem 是侧栏近期评论/博主动态用的展示模型。
type CommentWidgetItem struct {
	Comment    model.Comment
	Post       model.Post
	CommentURL string
	AuthorURL  string
	Snippet    string
}

// ListPostsResult 是分页文章列表结果。
type ListPostsResult struct {
	Posts []model.Post
	Total int64
	Page  int
	Pages int
}

// ListPosts 返回已发布文章的分页列表(按发布时间倒序),预加载分类/标签。
// 可选 categorySlug / tagSlug 过滤。
func (s *Store) ListPosts(page, pageSize int, categorySlug, tagSlug string) (*ListPostsResult, error) {
	if page < 1 {
		page = 1
	}
	q := s.db.Omit("CommentCount").Model(&model.Post{}).
		Where("post_type = ? AND status = ?", model.PostTypePost, model.StatusPublished)

	if categorySlug != "" {
		q = q.Joins("JOIN post_categories pc ON pc.post_id = posts.id").
			Joins("JOIN categories c ON c.id = pc.category_id").
			Where("c.slug = ?", categorySlug)
	}
	if tagSlug != "" {
		q = q.Joins("JOIN post_tags pt ON pt.post_id = posts.id").
			Joins("JOIN tags t ON t.id = pt.tag_id").
			Where("t.slug = ?", tagSlug)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, errors.Wrap(err, "count posts")
	}

	var posts []model.Post
	err := q.Preload("Categories").Preload("Tags").Preload("Author").
		Order("published_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&posts).Error
	if err != nil {
		return nil, errors.Wrap(err, "list posts")
	}
	s.fillCommentCounts(posts)

	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	return &ListPostsResult{Posts: posts, Total: total, Page: page, Pages: pages}, nil
}

// SearchPosts 在标题/正文中做 LIKE 模糊搜索(仅已发布文章)。
func (s *Store) SearchPosts(keyword string, page, pageSize int) (*ListPostsResult, error) {
	if page < 1 {
		page = 1
	}
	like := "%" + keyword + "%"
	q := s.db.Omit("CommentCount").Model(&model.Post{}).
		Where("post_type = ? AND status = ?", model.PostTypePost, model.StatusPublished).
		Where("title LIKE ? OR content LIKE ?", like, like)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, errors.Wrap(err, "count search")
	}
	var posts []model.Post
	err := q.Preload("Categories").Preload("Tags").Preload("Author").
		Order("published_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&posts).Error
	if err != nil {
		return nil, errors.Wrap(err, "search posts")
	}
	s.fillCommentCounts(posts)

	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	return &ListPostsResult{Posts: posts, Total: total, Page: page, Pages: pages}, nil
}

// fillCommentCounts 为列表里的每篇文章填充已批准评论数(CommentCount,不入库)。
func (s *Store) fillCommentCounts(posts []model.Post) {
	if len(posts) == 0 {
		return
	}
	ids := make([]uint, len(posts))
	for i := range posts {
		ids[i] = posts[i].ID
	}
	counts := s.ApprovedCommentCounts(ids)
	for i := range posts {
		posts[i].CommentCount = counts[posts[i].ID]
	}
}

// ApprovedCommentCounts 批量统计若干文章的已批准评论数。
func (s *Store) ApprovedCommentCounts(postIDs []uint) map[uint]int64 {
	out := map[uint]int64{}
	if len(postIDs) == 0 {
		return out
	}
	type row struct {
		PostID uint
		N      int64
	}
	var rows []row
	s.db.Model(&model.Comment{}).
		Select("post_id, COUNT(*) AS n").
		Where("post_id IN ? AND status = ?", postIDs, model.CommentApproved).
		Group("post_id").Scan(&rows)
	for _, r := range rows {
		out[r.PostID] = r.N
	}
	return out
}

// GetPostByID 按 ID 取已发布文章(含分类/标签/作者)。
func (s *Store) GetPostByID(id uint) (*model.Post, error) {
	var p model.Post
	err := s.db.Omit("CommentCount").Preload("Categories").Preload("Tags").Preload("Author").
		Where("id = ? AND status = ?", id, model.StatusPublished).
		First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ResolvePostByPath 根据解析后的固定链接条件定位已发布文章。
//
// 注意：这里不会仅凭某一个字段直接认定命中，而是先做数据库过滤，再用
// permalink.Post 的结果与原路径做一次精确比对，确保“当前规则生成出来的路径”
// 与用户请求完全一致。
func (s *Store) ResolvePostByPath(path string, match *permalink.PostPathMatch) (*model.Post, error) {
	if match == nil {
		return nil, gorm.ErrRecordNotFound
	}
	q := s.db.Omit("CommentCount").Model(&model.Post{}).
		Where("post_type = ? AND status = ?", model.PostTypePost, model.StatusPublished)
	if match.HasPostID {
		q = q.Where("posts.id = ?", match.PostID)
	}
	if match.HasName {
		q = q.Where("posts.slug = ?", match.PostName)
	}
	if match.HasYear {
		q = q.Where("strftime('%Y', posts.published_at) = ?", fmt.Sprintf("%04d", match.Year))
	}
	if match.HasMonth {
		q = q.Where("strftime('%m', posts.published_at) = ?", fmt.Sprintf("%02d", match.Month))
	}
	if match.HasDay {
		q = q.Where("strftime('%d', posts.published_at) = ?", fmt.Sprintf("%02d", match.Day))
	}
	if match.HasHour {
		q = q.Where("strftime('%H', posts.published_at) = ?", fmt.Sprintf("%02d", match.Hour))
	}
	if match.HasMinute {
		q = q.Where("strftime('%M', posts.published_at) = ?", fmt.Sprintf("%02d", match.Minute))
	}
	if match.HasSecond {
		q = q.Where("strftime('%S', posts.published_at) = ?", fmt.Sprintf("%02d", match.Second))
	}
	if match.HasAuthor {
		q = q.Joins("JOIN users ON users.id = posts.author_id").Where("users.username = ?", match.Author)
	}
	if match.HasCat {
		q = q.Joins("JOIN post_categories pc_path ON pc_path.post_id = posts.id").
			Joins("JOIN categories c_path ON c_path.id = pc_path.category_id").
			Where("c_path.slug = ?", match.Category)
	}
	var posts []model.Post
	err := q.Preload("Categories").Preload("Tags").Preload("Author").
		Order("published_at DESC").Order("id DESC").
		Limit(20).
		Find(&posts).Error
	if err != nil {
		return nil, errors.Wrap(err, "resolve post by path")
	}
	for i := range posts {
		if permalink.Post(&posts[i]) == path {
			return &posts[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// GetPostAnyStatus 按 ID 取任意状态文章(含分类/标签/作者),用于作者预览草稿。
func (s *Store) GetPostAnyStatus(id uint) (*model.Post, error) {
	var p model.Post
	err := s.db.Omit("CommentCount").Preload("Categories").Preload("Tags").Preload("Author").
		Where("id = ? AND post_type = ?", id, model.PostTypePost).
		First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PrevPost 返回比 t 更早的最近一篇已发布文章(上一篇)。无则返回 nil。
func (s *Store) PrevPost(t time.Time) *model.Post {
	var p model.Post
	err := s.db.Select("id", "title", "slug", "published_at", "author_id").
		Preload("Categories").Preload("Author").
		Where("post_type = ? AND status = ? AND published_at < ?",
			model.PostTypePost, model.StatusPublished, t).
		Order("published_at DESC").First(&p).Error
	if err != nil {
		return nil
	}
	return &p
}

// NextPost 返回比 t 更晚的最近一篇已发布文章(下一篇)。无则返回 nil。
func (s *Store) NextPost(t time.Time) *model.Post {
	var p model.Post
	err := s.db.Select("id", "title", "slug", "published_at", "author_id").
		Preload("Categories").Preload("Author").
		Where("post_type = ? AND status = ? AND published_at > ?",
			model.PostTypePost, model.StatusPublished, t).
		Order("published_at ASC").First(&p).Error
	if err != nil {
		return nil
	}
	return &p
}

// GetPageBySlug 按 slug 取页面(post_type=page)。
func (s *Store) GetPageBySlug(slug string) (*model.Post, error) {
	var p model.Post
	err := s.db.Omit("CommentCount").Preload("Author").Where("slug = ? AND post_type = ? AND status = ?",
		slug, model.PostTypePage, model.StatusPublished).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// IncrementViews 给文章浏览量 +1。
func (s *Store) IncrementViews(id uint) error {
	return s.db.Model(&model.Post{}).Where("id = ?", id).
		UpdateColumn("views", gorm.Expr("views + 1")).Error
}

// AllPostsForArchive 返回全部已发布文章(仅 id/title/published_at),用于归档页。
func (s *Store) AllPostsForArchive() ([]model.Post, error) {
	var posts []model.Post
	err := s.db.Select("id", "title", "slug", "published_at", "author_id").
		Preload("Categories").Preload("Author").
		Where("post_type = ? AND status = ?", model.PostTypePost, model.StatusPublished).
		Order("published_at DESC").Find(&posts).Error
	if err != nil {
		return nil, errors.Wrap(err, "archive posts")
	}
	return posts, nil
}

// ApprovedComments 返回某文章的已审核评论(按时间正序)。
func (s *Store) ApprovedComments(postID uint) ([]model.Comment, error) {
	var comments []model.Comment
	err := s.db.Where("post_id = ? AND status = ?", postID, model.CommentApproved).
		Order("created_at ASC").Find(&comments).Error
	if err != nil {
		return nil, errors.Wrap(err, "approved comments")
	}
	return comments, nil
}

// ApprovedCommentsPage 按“顶层评论”分页返回已审核评论。
// 顶层评论按 created_at DESC(最新评论在前),子评论跟随父评论并按 created_at ASC。
func (s *Store) ApprovedCommentsPage(postID uint, page, pageSize int) (*CommentPageResult, error) {
	return s.VisibleCommentsPageForViewer(postID, page, pageSize, 0, nil)
}

// VisibleCommentsPage 按“顶层评论”分页返回当前访问者可见的评论。
// 所有人都能看到 approved 评论;登录用户还能看到自己提交的 pending 评论。
func (s *Store) VisibleCommentsPage(postID uint, page, pageSize int, currentUserID uint) (*CommentPageResult, error) {
	return s.VisibleCommentsPageForViewer(postID, page, pageSize, currentUserID, nil)
}

// VisibleCommentsPageForViewer 按“顶层评论”分页返回当前访问者可见的评论。
// pendingCommentIDs 用于让匿名访问者在当前会话中看到自己刚提交的待审评论。
func (s *Store) VisibleCommentsPageForViewer(postID uint, page, pageSize int, currentUserID uint, pendingCommentIDs []uint) (*CommentPageResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	var totalAll int64
	if err := visibleCommentsQuery(s.db.Model(&model.Comment{}), postID, currentUserID, pendingCommentIDs).
		Count(&totalAll).Error; err != nil {
		return nil, errors.Wrap(err, "count visible comments")
	}

	qTop := visibleCommentsQuery(s.db.Model(&model.Comment{}), postID, currentUserID, pendingCommentIDs).
		Where("parent_id = 0")
	var totalTop int64
	if err := qTop.Count(&totalTop).Error; err != nil {
		return nil, errors.Wrap(err, "count top comments")
	}
	pages := int((totalTop + int64(pageSize) - 1) / int64(pageSize))
	if pages == 0 {
		pages = 1
	}
	if page > pages {
		page = pages
	}

	var tops []model.Comment
	err := qTop.Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&tops).Error
	if err != nil {
		return nil, errors.Wrap(err, "list top comments")
	}
	if len(tops) == 0 {
		return &CommentPageResult{Page: page, Pages: pages, TotalTop: totalTop, TotalComments: totalAll}, nil
	}

	ids := make([]uint, len(tops))
	index := make(map[uint][]model.Comment, len(tops))
	for i := range tops {
		ids[i] = tops[i].ID
	}
	var children []model.Comment
	if err := visibleCommentsQuery(s.db.Model(&model.Comment{}), postID, currentUserID, pendingCommentIDs).
		Where("parent_id IN ?", ids).
		Order("created_at ASC").Find(&children).Error; err != nil {
		return nil, errors.Wrap(err, "list child comments")
	}
	for _, c := range children {
		index[c.ParentID] = append(index[c.ParentID], c)
	}

	out := make([]model.Comment, 0, len(tops)+len(children))
	for _, top := range tops {
		out = append(out, top)
		out = append(out, index[top.ID]...)
	}
	populateReplyToAuthors(s.db, out)
	return &CommentPageResult{Comments: out, TotalComments: totalAll, TotalTop: totalTop, Page: page, Pages: pages}, nil
}

func populateReplyToAuthors(db *gorm.DB, comments []model.Comment) {
	ids := make([]uint, 0)
	seen := map[uint]bool{}
	for _, c := range comments {
		if c.ReplyToID == 0 || c.ReplyToID == c.ParentID || seen[c.ReplyToID] {
			continue
		}
		seen[c.ReplyToID] = true
		ids = append(ids, c.ReplyToID)
	}
	if len(ids) == 0 {
		return
	}
	var targets []model.Comment
	if err := db.Select("id", "author").Where("id IN ?", ids).Find(&targets).Error; err != nil {
		return
	}
	authors := make(map[uint]string, len(targets))
	for _, target := range targets {
		authors[target.ID] = target.Author
	}
	for i := range comments {
		comments[i].ReplyToAuthor = authors[comments[i].ReplyToID]
	}
}

func visibleCommentsQuery(q *gorm.DB, postID uint, currentUserID uint, pendingCommentIDs []uint) *gorm.DB {
	if currentUserID == 0 && len(pendingCommentIDs) == 0 {
		return q.Where("post_id = ? AND status = ?", postID, model.CommentApproved)
	}
	where := "post_id = ? AND (status = ?"
	args := []any{postID, model.CommentApproved}
	if currentUserID != 0 {
		where += " OR (status = ? AND user_id = ?)"
		args = append(args, model.CommentPending, currentUserID)
	}
	if len(pendingCommentIDs) > 0 {
		where += " OR (status = ? AND id IN ?)"
		args = append(args, model.CommentPending, pendingCommentIDs)
	}
	where += ")"
	return q.Where(where, args...)
}

// CommentPageForID 返回某条评论所在的评论页(按顶层评论分页,最新评论在前)。
// 不存在时返回 1。
func (s *Store) CommentPageForID(commentID uint, pageSize int) int {
	return s.VisibleCommentPageForViewerID(commentID, pageSize, 0, nil)
}

// VisibleCommentPageForID 返回某条可见评论所在的评论页。
// approved 评论对所有人可见;pending 评论仅对评论人本人可见。
func (s *Store) VisibleCommentPageForID(commentID uint, pageSize int, currentUserID uint) int {
	return s.VisibleCommentPageForViewerID(commentID, pageSize, currentUserID, nil)
}

// VisibleCommentPageForViewerID 返回某条当前访问者可见评论所在的评论页。
func (s *Store) VisibleCommentPageForViewerID(commentID uint, pageSize int, currentUserID uint, pendingCommentIDs []uint) int {
	if pageSize < 1 {
		pageSize = 20
	}
	var c model.Comment
	if err := s.db.Select("id", "post_id", "parent_id", "created_at", "status", "user_id").
		First(&c, commentID).
		Error; err != nil {
		return 1
	}
	if c.Status != model.CommentApproved && !commentVisibleToViewer(c, currentUserID, pendingCommentIDs) {
		return 1
	}
	rootID := c.ID
	rootCreatedAt := c.CreatedAt
	if c.ParentID != 0 {
		var parent model.Comment
		if err := s.db.Select("id", "created_at", "status", "user_id").
			First(&parent, c.ParentID).
			Error; err != nil {
			return 1
		}
		if parent.Status != model.CommentApproved && !commentVisibleToViewer(parent, currentUserID, pendingCommentIDs) {
			return 1
		}
		rootID = parent.ID
		rootCreatedAt = parent.CreatedAt
	}
	var newer int64
	_ = visibleCommentsQuery(s.db.Model(&model.Comment{}), c.PostID, currentUserID, pendingCommentIDs).
		Where("parent_id = 0").
		Where("created_at > ? OR (created_at = ? AND id > ?)", rootCreatedAt, rootCreatedAt, rootID).
		Count(&newer).Error
	return int(newer)/pageSize + 1
}

func commentVisibleToViewer(c model.Comment, currentUserID uint, pendingCommentIDs []uint) bool {
	if c.Status != model.CommentPending {
		return false
	}
	if currentUserID != 0 && c.UserID != nil && *c.UserID == currentUserID {
		return true
	}
	for _, id := range pendingCommentIDs {
		if id == c.ID {
			return true
		}
	}
	return false
}

// ResolveCommentReply 校验回复目标,并返回两层展示所需的 parent_id 与实际 reply_to_id。
func (s *Store) ResolveCommentReply(postID uint, replyToID uint, currentUserID uint, pendingCommentIDs []uint) (uint, uint, error) {
	if replyToID == 0 {
		return 0, 0, nil
	}
	var target model.Comment
	if err := s.db.Select("id", "post_id", "parent_id", "status", "user_id").First(&target, replyToID).Error; err != nil {
		return 0, 0, errors.Wrap(err, "get reply target")
	}
	if target.PostID != postID || (target.Status != model.CommentApproved && !commentVisibleToViewer(target, currentUserID, pendingCommentIDs)) {
		return 0, 0, errors.New("reply target not visible")
	}
	if target.ParentID == 0 {
		return target.ID, target.ID, nil
	}
	var parent model.Comment
	if err := s.db.Select("id", "post_id", "status", "user_id").First(&parent, target.ParentID).Error; err != nil {
		return 0, 0, errors.Wrap(err, "get reply parent")
	}
	if parent.PostID != postID || (parent.Status != model.CommentApproved && !commentVisibleToViewer(parent, currentUserID, pendingCommentIDs)) {
		return 0, 0, errors.New("reply parent not visible")
	}
	return parent.ID, target.ID, nil
}

// MenuPages 返回导航菜单页面(按 menu_order),用于头部导航。
func (s *Store) MenuPages() ([]model.Post, error) {
	var pages []model.Post
	err := s.db.Where("post_type = ? AND status = ? AND menu_order > 0",
		model.PostTypePage, model.StatusPublished).
		Order("menu_order ASC").Find(&pages).Error
	if err != nil {
		return nil, errors.Wrap(err, "menu pages")
	}
	return pages, nil
}

// PostMeta 取文章/页面的轻量字段(用于重定向、校验、小组件链接)。
func (s *Store) PostMeta(id uint) (*model.Post, error) {
	var p model.Post
	err := s.db.Select("id", "title", "published_at", "status", "post_type", "slug", "comment_status", "author_id").
		Preload("Categories").Preload("Author").
		Where("id = ?", id).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateComment 新建评论(默认 pending)。
func (s *Store) CreateComment(c *model.Comment) error {
	return errors.Wrap(s.db.Create(c).Error, "create comment")
}

// RecentCommentCountByIP 返回某 IP 在最近一段时间内的评论数(用于限频)。
func (s *Store) RecentCommentCountByIP(ip string, sinceUnix int64) (int64, error) {
	var n int64
	err := s.db.Model(&model.Comment{}).
		Where("ip = ? AND created_at > datetime(?, 'unixepoch')", ip, sinceUnix).
		Count(&n).Error
	return n, errors.Wrap(err, "recent comment count")
}

// RecentComments 返回最近 n 条已批准评论(侧栏小组件)。
func (s *Store) RecentComments(n int) []model.Comment {
	var cs []model.Comment
	s.db.Where("status = ?", model.CommentApproved).
		Order("created_at DESC").Limit(n).Find(&cs)
	return cs
}

// RecentCommentItems 返回近期评论的小组件展示模型。
func (s *Store) RecentCommentItems(n, pageSize int) []CommentWidgetItem {
	comments := s.RecentComments(n)
	return s.buildCommentWidgetItems(comments, pageSize)
}

// RecentPosts 返回最近 n 篇已发布文章(仅 id/title/published_at,侧栏小组件)。
func (s *Store) RecentPosts(n int) []model.Post {
	var ps []model.Post
	s.db.Select("id", "title", "slug", "published_at", "author_id").
		Preload("Categories").Preload("Author").
		Where("post_type = ? AND status = ?", model.PostTypePost, model.StatusPublished).
		Order("published_at DESC").Limit(n).Find(&ps)
	return ps
}

// SayingComments 返回指定 post_id 下博主本人发表的最近 n 条评论。
// 「博主本人」= 评论 email 命中 users 表。
func (s *Store) SayingComments(postID uint, n int) []model.Comment {
	var cs []model.Comment
	s.db.Where("post_id = ? AND status = ? AND email IN (?)",
		postID, model.CommentApproved,
		s.db.Model(&model.User{}).Select("email")).
		Order("created_at DESC").Limit(n).Find(&cs)
	return cs
}

// SayingCommentItems 返回博主动态的小组件展示模型。
func (s *Store) SayingCommentItems(postID uint, n, pageSize int) []CommentWidgetItem {
	comments := s.SayingComments(postID, n)
	return s.buildCommentWidgetItems(comments, pageSize)
}

func (s *Store) buildCommentWidgetItems(comments []model.Comment, pageSize int) []CommentWidgetItem {
	items := make([]CommentWidgetItem, 0, len(comments))
	for _, c := range comments {
		p, err := s.PostMeta(c.PostID)
		if err != nil || p.Status != model.StatusPublished {
			continue
		}
		base := "/"
		if p.PostType == model.PostTypePage {
			base = permalink.Page(p)
		} else {
			base = permalink.Post(p)
		}
		cpage := s.CommentPageForID(c.ID, pageSize)
		items = append(items, CommentWidgetItem{
			Comment:    c,
			Post:       *p,
			CommentURL: base + "?cpage=" + strconv.Itoa(cpage) + "#comment-" + strconv.Itoa(int(c.ID)),
			AuthorURL:  strings.TrimSpace(c.URL),
			Snippet:    commentSnippet(c.Content, 36),
		})
	}
	return items
}

func commentSnippet(s string, maxRunes int) string {
	s = strings.TrimSpace(stripTags(s))
	if maxRunes < 1 {
		return s
	}
	var out []rune
	for _, r := range []rune(s) {
		if len(out) >= maxRunes {
			return string(out) + "…"
		}
		out = append(out, r)
	}
	return string(out)
}

func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// ArchiveMonth 是归档下拉的一项(年月 + 计数)。
type ArchiveMonth struct {
	Year  int
	Month int
	Count int64
}

// ArchiveMonths 返回按年月分组的已发布文章数(倒序),用于归档下拉。
func (s *Store) ArchiveMonths() []ArchiveMonth {
	type row struct {
		Ym string
		N  int64
	}
	var rows []row
	s.db.Model(&model.Post{}).
		Select("strftime('%Y-%m', published_at) AS ym, COUNT(*) AS n").
		Where("post_type = ? AND status = ?", model.PostTypePost, model.StatusPublished).
		Group("ym").Order("ym DESC").Scan(&rows)
	out := make([]ArchiveMonth, 0, len(rows))
	for _, r := range rows {
		if len(r.Ym) != 7 {
			continue
		}
		y, _ := strconv.Atoi(r.Ym[:4])
		m, _ := strconv.Atoi(r.Ym[5:7])
		out = append(out, ArchiveMonth{Year: y, Month: m, Count: r.N})
	}
	return out
}

// AllCategories 返回全部分类(按名),用于侧栏下拉/后台勾选。
func (s *Store) AllCategories() []model.Category {
	var cs []model.Category
	s.db.Order("name ASC").Find(&cs)
	return cs
}

// AllTags 返回全部标签(按名)。
func (s *Store) AllTags() []model.Tag {
	var ts []model.Tag
	s.db.Order("name ASC").Find(&ts)
	return ts
}
