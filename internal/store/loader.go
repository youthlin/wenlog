package store

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"gorm.io/gorm"
)

// DataLoader 全量加载数据库到内存，后续查询均为内存操作。
// 适用于个人博客数据量小的场景（< 10 万行）。
type DataLoader struct {
	Posts      map[uint]*model.Post
	Comments   map[uint]*model.Comment
	Categories map[uint]*model.Category
	Tags       map[uint]*model.Tag
	Users      map[uint]*model.User
	Settings   map[string]string

	// 预计算索引
	postCategories    map[uint][]uint // postID → categoryIDs
	postTags          map[uint][]uint // postID → tagIDs
	commentsByPost    map[uint][]uint // postID → commentIDs (approved only, 公开)
	allCommentsByPost map[uint][]uint // postID → commentIDs (approved + pending, 含登录用户可见)
	postsBySlug       map[string]*model.Post
	postsByType    map[string][]*model.Post // "post" or "page"
	menuPages      []*model.Post
	archiveMonths  []ArchiveMonth
}

// LoadAll 全量加载所有公开数据到内存。
// 共 8 条 SQL：posts, comments, categories, tags, users, settings, post_categories, post_tags。
func (s *Store) LoadAll(ctx context.Context) (*DataLoader, error) {
	l := &DataLoader{}

	// 1. 全部已发布文章+页面
	var posts []model.Post
	if err := s.db(ctx).Where("status = ?", model.StatusPublished).
		Find(&posts).Error; err != nil {
		return nil, err
	}
	l.Posts = make(map[uint]*model.Post, len(posts))
	l.postsBySlug = make(map[string]*model.Post)
	l.postsByType = map[string][]*model.Post{
		model.PostTypePost: {},
		model.PostTypePage: {},
	}
	for i := range posts {
		p := &posts[i]
		l.Posts[p.ID] = p
		l.postsByType[p.PostType] = append(l.postsByType[p.PostType], p)
		if p.PostType == model.PostTypePage && p.Slug != "" {
			l.postsBySlug[p.Slug] = p
		}
	}

	// 2. 全部已批准+待审评论（登录用户可见自己的待审评论）
	var comments []model.Comment
	if err := s.db(ctx).Where("status IN ?", []string{model.CommentApproved, model.CommentPending}).
		Find(&comments).Error; err != nil {
		return nil, err
	}
	l.Comments = make(map[uint]*model.Comment, len(comments))
	l.commentsByPost = make(map[uint][]uint)
	l.allCommentsByPost = make(map[uint][]uint)
	for i := range comments {
		c := &comments[i]
		l.Comments[c.ID] = c
		l.allCommentsByPost[c.PostID] = append(l.allCommentsByPost[c.PostID], c.ID)
		if c.Status == model.CommentApproved {
			l.commentsByPost[c.PostID] = append(l.commentsByPost[c.PostID], c.ID)
		}
	}

	// 3. 全部分类
	var categories []model.Category
	if err := s.db(ctx).Find(&categories).Error; err != nil {
		return nil, err
	}
	l.Categories = make(map[uint]*model.Category, len(categories))
	for i := range categories {
		l.Categories[categories[i].ID] = &categories[i]
	}

	// 4. 全部标签
	var tags []model.Tag
	if err := s.db(ctx).Find(&tags).Error; err != nil {
		return nil, err
	}
	l.Tags = make(map[uint]*model.Tag, len(tags))
	for i := range tags {
		l.Tags[tags[i].ID] = &tags[i]
	}

	// 5. 全部用户
	var users []model.User
	if err := s.db(ctx).Find(&users).Error; err != nil {
		return nil, err
	}
	l.Users = make(map[uint]*model.User, len(users))
	for i := range users {
		l.Users[users[i].ID] = &users[i]
	}

	// 6. 全部设置
	var settings []model.Setting
	if err := s.db(ctx).Find(&settings).Error; err != nil {
		return nil, err
	}
	l.Settings = make(map[string]string, len(settings))
	for _, s := range settings {
		l.Settings[s.Key] = s.Value
	}

	// 7. post_categories 关联
	type pc struct {
		PostID     uint
		CategoryID uint
	}
	var pcs []pc
	if err := s.db(ctx).Table("post_categories").Find(&pcs).Error; err != nil {
		return nil, err
	}
	l.postCategories = make(map[uint][]uint)
	for _, r := range pcs {
		l.postCategories[r.PostID] = append(l.postCategories[r.PostID], r.CategoryID)
	}

	// 8. post_tags 关联
	type pt struct {
		PostID uint
		TagID  uint
	}
	var pts []pt
	if err := s.db(ctx).Table("post_tags").Find(&pts).Error; err != nil {
		return nil, err
	}
	l.postTags = make(map[uint][]uint)
	for _, r := range pts {
		l.postTags[r.PostID] = append(l.postTags[r.PostID], r.TagID)
	}

	// 构建预计算数据
	l.buildMenuPages()
	l.buildArchiveMonths()

	return l, nil
}

// FillPost 填充文章的关联数据（Author, Categories, Tags, CommentCount）。
func (l *DataLoader) FillPost(p *model.Post) {
	if l == nil || p == nil {
		return
	}
	if u, ok := l.Users[p.AuthorID]; ok {
		p.Author = *u
	}
	p.Categories = l.categoriesForPost(p.ID)
	p.Tags = l.tagsForPost(p.ID)
	p.CommentCount = int64(len(l.commentsByPost[p.ID]))
}

// FillPosts 批量填充。
func (l *DataLoader) FillPosts(posts []model.Post) {
	for i := range posts {
		l.FillPost(&posts[i])
	}
}

func (l *DataLoader) categoriesForPost(postID uint) []model.Category {
	ids := l.postCategories[postID]
	if len(ids) == 0 {
		return nil
	}
	result := make([]model.Category, 0, len(ids))
	for _, id := range ids {
		if c, ok := l.Categories[id]; ok {
			result = append(result, *c)
		}
	}
	return result
}

func (l *DataLoader) tagsForPost(postID uint) []model.Tag {
	ids := l.postTags[postID]
	if len(ids) == 0 {
		return nil
	}
	result := make([]model.Tag, 0, len(ids))
	for _, id := range ids {
		if t, ok := l.Tags[id]; ok {
			result = append(result, *t)
		}
	}
	return result
}

// GetSetting 从内存读取设置项。
func (l *DataLoader) GetSetting(key string) string {
	if l == nil {
		return ""
	}
	return l.Settings[key]
}

// GetSettings 批量读取设置项。
func (l *DataLoader) GetSettings(keys ...string) map[string]string {
	if l == nil {
		return nil
	}
	result := make(map[string]string, len(keys))
	for _, k := range keys {
		result[k] = l.Settings[k]
	}
	return result
}

// MenuPages 返回导航菜单页面（menu_order > 0 的已发布页面，按 menu_order 排序）。
func (l *DataLoader) MenuPages() []model.Post {
	if l == nil {
		return nil
	}
	result := make([]model.Post, len(l.menuPages))
	for i, p := range l.menuPages {
		result[i] = *p
	}
	return result
}

func (l *DataLoader) buildMenuPages() {
	var pages []*model.Post
	for _, p := range l.Posts {
		if p.PostType == model.PostTypePage && p.MenuOrder > 0 {
			pages = append(pages, p)
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].MenuOrder < pages[j].MenuOrder })
	l.menuPages = pages
}

// RecentPosts 返回最近 n 篇文章（按发布时间倒序）。
func (l *DataLoader) RecentPosts(n int) []model.Post {
	posts := l.postsByType[model.PostTypePost]
	sort.Slice(posts, func(i, j int) bool {
		return posts[i].PublishedAt.After(posts[j].PublishedAt)
	})
	if n > len(posts) {
		n = len(posts)
	}
	result := make([]model.Post, n)
	for i := 0; i < n; i++ {
		result[i] = *posts[i]
		l.FillPost(&result[i])
	}
	return result
}

// RecentComments 返回最近 n 条已批准评论（按创建时间倒序）。
func (l *DataLoader) RecentComments(n int) []model.Comment {
	all := l.allCommentsSorted()
	if n > len(all) {
		n = len(all)
	}
	result := make([]model.Comment, n)
	for i := 0; i < n; i++ {
		result[i] = *all[i]
	}
	return result
}

func (l *DataLoader) allCommentsSorted() []*model.Comment {
	result := make([]*model.Comment, 0, len(l.Comments))
	for _, c := range l.Comments {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

// SayingComments 返回指定文章下博主本人的评论。
func (l *DataLoader) SayingComments(postID uint, n int) []model.Comment {
	p, ok := l.Posts[postID]
	if !ok {
		return nil
	}
	authorID := p.AuthorID
	commentIDs := l.commentsByPost[postID]
	var result []model.Comment
	for _, cid := range commentIDs {
		c := l.Comments[cid]
		if c == nil || c.UserID == nil || *c.UserID != authorID {
			continue
		}
		result = append(result, *c)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	if n > len(result) {
		n = len(result)
	}
	return result[:n]
}

// AllCategories 返回全部分类（按名称排序）。
func (l *DataLoader) AllCategories() []model.Category {
	result := make([]model.Category, 0, len(l.Categories))
	for _, c := range l.Categories {
		result = append(result, *c)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// AllTags 返回全部标签（按名称排序）。
func (l *DataLoader) AllTags() []model.Tag {
	result := make([]model.Tag, 0, len(l.Tags))
	for _, t := range l.Tags {
		result = append(result, *t)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// ArchiveMonths 返回归档月份统计。
func (l *DataLoader) ArchiveMonths() []ArchiveMonth {
	return l.archiveMonths
}

func (l *DataLoader) buildArchiveMonths() {
	type ym struct {
		Year  int
		Month int
	}
	counts := map[ym]int64{}
	for _, p := range l.Posts {
		if p.PostType != model.PostTypePost {
			continue
		}
		key := ym{Year: p.PublishedAt.Year(), Month: int(p.PublishedAt.Month())}
		counts[key]++
	}
	result := make([]ArchiveMonth, 0, len(counts))
	for k, v := range counts {
		result = append(result, ArchiveMonth{Year: k.Year, Month: k.Month, Count: v})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Year != result[j].Year {
			return result[i].Year > result[j].Year
		}
		return result[i].Month > result[j].Month
	})
	l.archiveMonths = result
}

// GetPageBySlug 按 slug 查找已发布页面。
func (l *DataLoader) GetPageBySlug(slug string) *model.Post {
	return l.postsBySlug[slug]
}

// PostsByType 返回指定类型的文章列表（"post" 或 "page"）。
func (l *DataLoader) PostsByType(postType string) []*model.Post {
	return l.postsByType[postType]
}

// PostCategoryIDs 返回文章的分类 ID 列表。
func (l *DataLoader) PostCategoryIDs(postID uint) []uint {
	return l.postCategories[postID]
}

// PostTagIDs 返回文章的标签 ID 列表。
func (l *DataLoader) PostTagIDs(postID uint) []uint {
	return l.postTags[postID]
}

// CommentIDsByPost 返回文章的已批准评论 ID 列表。
func (l *DataLoader) CommentIDsByPost(postID uint) []uint {
	return l.commentsByPost[postID]
}

// GetPostByID 按 ID 查找已发布文章。
func (l *DataLoader) GetPostByID(id uint) *model.Post {
	p := l.Posts[id]
	if p == nil || p.PostType != model.PostTypePost {
		return nil
	}
	return p
}

// PostMeta 返回文章元数据（不含 Content）。
func (l *DataLoader) PostMeta(id uint) *model.Post {
	return l.Posts[id]
}

// PostMetas 批量返回文章元数据。
func (l *DataLoader) PostMetas(ids []uint) map[uint]*model.Post {
	result := make(map[uint]*model.Post, len(ids))
	for _, id := range ids {
		if p := l.Posts[id]; p != nil {
			result[id] = p
		}
	}
	return result
}

// PrevPost 返回前一篇文章。
func (l *DataLoader) PrevPost(t time.Time) *model.Post {
	posts := l.postsByType[model.PostTypePost]
	var prev *model.Post
	for _, p := range posts {
		if p.PublishedAt.Before(t) {
			if prev == nil || p.PublishedAt.After(prev.PublishedAt) {
				prev = p
			}
		}
	}
	return prev
}

// NextPost 返回后一篇文章。
func (l *DataLoader) NextPost(t time.Time) *model.Post {
	posts := l.postsByType[model.PostTypePost]
	var next *model.Post
	for _, p := range posts {
		if p.PublishedAt.After(t) {
			if next == nil || p.PublishedAt.Before(next.PublishedAt) {
				next = p
			}
		}
	}
	return next
}

// ListPosts 从内存中分页列出文章（支持分类/标签过滤）。
func (l *DataLoader) ListPosts(page, pageSize int, categorySlug, tagSlug string) *ListPostsResult {
	posts := l.postsByType[model.PostTypePost]

	// 按发布时间倒序
	sorted := make([]*model.Post, len(posts))
	copy(sorted, posts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PublishedAt.After(sorted[j].PublishedAt)
	})

	// 分类过滤
	if categorySlug != "" {
		var catID uint
		for _, c := range l.Categories {
			if c.Slug == categorySlug {
				catID = c.ID
				break
			}
		}
		if catID != 0 {
			filtered := make([]*model.Post, 0)
			for _, p := range sorted {
				for _, cid := range l.postCategories[p.ID] {
					if cid == catID {
						filtered = append(filtered, p)
						break
					}
				}
			}
			sorted = filtered
		}
	}

	// 标签过滤
	if tagSlug != "" {
		var tagID uint
		for _, t := range l.Tags {
			if t.Slug == tagSlug {
				tagID = t.ID
				break
			}
		}
		if tagID != 0 {
			filtered := make([]*model.Post, 0)
			for _, p := range sorted {
				for _, tid := range l.postTags[p.ID] {
					if tid == tagID {
						filtered = append(filtered, p)
						break
					}
				}
			}
			sorted = filtered
		}
	}

	total := int64(len(sorted))
	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if page < 1 {
		page = 1
	}
	if page > pages && pages > 0 {
		page = pages
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(sorted) {
		end = len(sorted)
	}

	result := make([]model.Post, 0, end-start)
	for i := start; i < end; i++ {
		p := *sorted[i]
		l.FillPost(&p)
		result = append(result, p)
	}

	return &ListPostsResult{Posts: result, Total: total, Page: page, Pages: pages}
}

// AllPostsForArchive 返回归档所需的全部文章（按发布时间倒序）。
func (l *DataLoader) AllPostsForArchive() []model.Post {
	posts := l.postsByType[model.PostTypePost]
	sorted := make([]*model.Post, len(posts))
	copy(sorted, posts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PublishedAt.After(sorted[j].PublishedAt)
	})
	result := make([]model.Post, len(sorted))
	for i, p := range sorted {
		result[i] = *p
		l.FillPost(&result[i])
	}
	return result
}

// CommentWidgetItems 从内存构建评论 widget 数据。
func (l *DataLoader) CommentWidgetItems(comments []model.Comment) []CommentWidgetItem {
	if len(comments) == 0 {
		return nil
	}
	items := make([]CommentWidgetItem, 0, len(comments))
	for i := range comments {
		c := &comments[i]
		p := l.Posts[c.PostID]
		if p == nil {
			continue
		}
		base := "/"
		if p.PostType == model.PostTypePage {
			base = permalink.Page(p)
		} else {
			base = permalink.Post(p)
		}
		items = append(items, CommentWidgetItem{
			Comment:    *c,
			Post:       *p,
			CommentURL: base + "#comment-" + strconv.Itoa(int(c.ID)),
			AuthorURL:  strings.TrimSpace(c.URL),
			Snippet:    commentSnippet(c.Content, 100),
		})
	}
	return items
}

// CommentPage 从内存返回文章评论分页。
// currentUserID: 0 表示匿名访客（只看 approved），非 0 时额外可见自己的 pending 评论。
func (l *DataLoader) CommentPage(postID uint, page, pageSize int, currentUserID uint) *CommentPageResult {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	// 选择索引：登录用户用 allCommentsByPost，匿名用 commentsByPost
	commentIDs := l.commentsByPost[postID]
	if currentUserID != 0 {
		commentIDs = l.allCommentsByPost[postID]
	}
	allComments := make([]*model.Comment, 0, len(commentIDs))
	for _, cid := range commentIDs {
		c := l.Comments[cid]
		if c == nil {
			continue
		}
		// 登录用户：approved + 自己的 pending
		if currentUserID != 0 && c.Status == model.CommentPending && (c.UserID == nil || *c.UserID != currentUserID) {
			continue
		}
		allComments = append(allComments, c)
	}

	// 按 parent_id 分组
	byParent := make(map[uint][]*model.Comment)
	for _, c := range allComments {
		byParent[c.ParentID] = append(byParent[c.ParentID], c)
	}

	// 顶层评论按时间倒序
	tops := byParent[0]
	sort.Slice(tops, func(i, j int) bool {
		return tops[i].CreatedAt.After(tops[j].CreatedAt)
	})

	totalAll := int64(len(allComments))
	totalTop := int64(len(tops))
	pages := int((totalTop + int64(pageSize) - 1) / int64(pageSize))
	if pages == 0 {
		pages = 1
	}
	if page > pages {
		page = pages
	}

	// 分页取顶层评论
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(tops) {
		end = len(tops)
	}
	pageTops := tops[start:end]

	if len(pageTops) == 0 {
		return &CommentPageResult{Page: page, Pages: pages, TotalTop: totalTop, TotalComments: totalAll}
	}

	// 递归收集所有子孙评论
	rootByID := make(map[uint]uint)
	for _, top := range pageTops {
		rootByID[top.ID] = top.ID
	}

	var allDescendants []model.Comment
	frontier := make([]uint, len(pageTops))
	for i, top := range pageTops {
		frontier[i] = top.ID
	}
	for len(frontier) > 0 {
		var nextGen []*model.Comment
		for _, pid := range frontier {
			nextGen = append(nextGen, byParent[pid]...)
		}
		sort.Slice(nextGen, func(i, j int) bool {
			return nextGen[i].CreatedAt.Before(nextGen[j].CreatedAt)
		})
		frontier = frontier[:0]
		for _, c := range nextGen {
			if _, seen := rootByID[c.ID]; seen {
				continue
			}
			rootByID[c.ID] = rootByID[c.ParentID]
			allDescendants = append(allDescendants, *c)
			frontier = append(frontier, c.ID)
		}
	}

	// 将深度 >1 的子孙评论 ParentID 重写为根顶层评论 ID
	for i := range allDescendants {
		origParent := allDescendants[i].ParentID
		allDescendants[i].ParentID = rootByID[allDescendants[i].ParentID]
		if allDescendants[i].ReplyToID == 0 {
			allDescendants[i].ReplyToID = origParent
		}
	}

	// 组装结果：顶层评论 + 子孙评论
	result := make([]model.Comment, 0, len(pageTops)+len(allDescendants))
	for _, top := range pageTops {
		result = append(result, *top)
	}
	result = append(result, allDescendants...)

	return &CommentPageResult{
		Comments:      result,
		TotalComments: totalAll,
		TotalTop:      totalTop,
		Page:          page,
		Pages:         pages,
	}
}

// ResolvePostByPath 从内存解析文章路径，替代 store.ResolvePostByPath 的 DB 查询。
func (l *DataLoader) ResolvePostByPath(path string, match *permalink.PostPathMatch) (*model.Post, error) {
	if match == nil {
		return nil, gorm.ErrRecordNotFound
	}
	// 有 postID 时直接查内存
	if match.HasPostID {
		p := l.Posts[match.PostID]
		if p == nil || p.PostType != model.PostTypePost || p.Status != model.StatusPublished {
			return nil, gorm.ErrRecordNotFound
		}
		if permalink.Post(p) == path {
			return p, nil
		}
		return nil, gorm.ErrRecordNotFound
	}
	// 无 postID 时遍历所有已发布文章匹配路径
	for _, p := range l.Posts {
		if p.PostType != model.PostTypePost || p.Status != model.StatusPublished {
			continue
		}
		if permalink.Post(p) == path {
			return p, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// LoadAllCached 返回缓存的 DataLoader，缓存未命中时自动重建。
// 这是前台 handler 的首选方法——首次请求加载后，后续请求直接复用。
func (s *Store) LoadAllCached(ctx context.Context) (*DataLoader, error) {
	s.cacheMu.RLock()
	c := s.cache
	s.cacheMu.RUnlock()
	if c != nil {
		cacheHits.Inc()
		return c, nil
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	// double-check
	if s.cache != nil {
		cacheHits.Inc()
		return s.cache, nil
	}
	cacheMisses.Inc()
	l, err := s.LoadAll(ctx)
	if err != nil {
		return nil, err
	}
	s.cache = l
	return l, nil
}

// InvalidateCache 使缓存失效。写操作（发布/编辑/删除文章、审核评论、修改设置等）后调用。
func (s *Store) InvalidateCache() {
	s.cacheMu.Lock()
	s.cache = nil
	s.cacheMu.Unlock()
}
