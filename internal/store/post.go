// Package store — 文章/页面相关方法。
package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"gorm.io/gorm"
)

func (s *Store) PageSlugExists(ctx context.Context, slug string, excludeID uint) (bool, error) {
	var n int64
	q := s.DB(ctx).Model(&model.Post{}).Where("post_type = ? AND slug = ?", model.PostTypePage, slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count page slug")
}
func (s *Store) PostSlugExists(ctx context.Context, slug string, excludeID uint) (bool, error) {
	var n int64
	q := s.DB(ctx).Model(&model.Post{}).Where("post_type = ? AND slug = ?", model.PostTypePost, slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count post slug")
}
func (s *Store) CountPosts(ctx context.Context) (int64, error) {
	var total int64
	err := s.DB(ctx).Model(&model.Post{}).Count(&total).Error
	return total, errors.Wrap(err, "count posts")
}
func (s *Store) AdminListPosts(ctx context.Context, postType string, page, pageSize int, categoryID, tagID uint, keyword string) ([]model.Post, int64, error) {
	return s.AdminListPostsForAuthor(ctx, postType, page, pageSize, categoryID, tagID, keyword, 0)
}
func (s *Store) AdminListPostsForAuthor(ctx context.Context, postType string, page, pageSize int, categoryID, tagID uint, keyword string, authorID uint) ([]model.Post, int64, error) {
	q := s.DB(ctx).Model(&model.Post{}).Where("post_type = ?", postType)
	if authorID > 0 {
		q = q.Where("author_id = ?", authorID)
	}
	if categoryID > 0 {
		q = q.Joins("JOIN post_categories pc_admin ON pc_admin.post_id = posts.id").Where("pc_admin.category_id = ?", categoryID)
	}
	if tagID > 0 {
		q = q.Joins("JOIN post_tags pt_admin ON pt_admin.post_id = posts.id").Where("pt_admin.tag_id = ?", tagID)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := termQueryLike(kw)
		q = q.Where("LOWER(title) LIKE ? OR LOWER(slug) LIKE ? OR LOWER(content_md) LIKE ? OR LOWER(content) LIKE ?", like, like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count")
	}
	var posts []model.Post
	commentCount := s.DB(ctx).Model(&model.Comment{}).
		Select("COUNT(1)").
		Where("comments.post_id = posts.id")
	err := q.Select("posts.*, (?) AS comment_count", commentCount).
		Preload("Categories").Preload("Tags").Preload("Author").
		Scopes(adminPostOrder(postType)).
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&posts).Error
	return posts, total, errors.Wrap(err, "admin list posts")
}
func (s *Store) AdminGetPost(ctx context.Context, id uint) (*model.Post, error) {
	var p model.Post
	err := s.DB(ctx).Preload("Categories").Preload("Tags").First(&p, id).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}
func (s *Store) NextPostID(ctx context.Context) (uint, error) {
	var maxID uint
	row := s.DB(ctx).Model(&model.Post{}).Select("COALESCE(MAX(id),0)").Row()
	if err := row.Scan(&maxID); err != nil {
		return 0, errors.Wrap(err, "max post id")
	}
	return maxID + 1, nil
}
func (s *Store) SavePost(ctx context.Context, p *model.Post) error {
	defer s.InvalidateCache()
	return errors.Wrap(s.DB(ctx).Save(p).Error, "保存文章失败")
}
func (s *Store) SavePostWithTerms(ctx context.Context, p *model.Post, catIDs []uint, tagNames []string) error {
	defer s.InvalidateCache()
	return s.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(p).Error; err != nil {
			return errors.Wrap(err, "保存文章失败")
		}
		// 分类:按 ID 取。
		var cats []model.Category
		if len(catIDs) > 0 {
			if err := tx.Where("id IN ?", catIDs).Find(&cats).Error; err != nil {
				return errors.Wrapf(err, "查询文章分类失败: %v", catIDs)
			}
		}
		if err := tx.Model(p).Association("Categories").Replace(cats); err != nil {
			return errors.Wrapf(err, "记录文章-分类关系失败: %v", catIDs)
		}
		// 标签:按 slug(唯一键)查找或新建。
		var tags []model.Tag
		for _, name := range tagNames {
			slug := slugifyTag(name)
			var t model.Tag
			err := tx.Where("slug = ?", slug).First(&t).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				t = model.Tag{Name: name, Slug: slug}
				if err = tx.Create(&t).Error; err != nil {
					return errors.Wrapf(err, "创建标签[%s]失败", name)
				}
			} else if err != nil {
				return errors.Wrapf(err, "查询标签[%s]失败", name)
			}
			tags = append(tags, t)
		}
		if err := tx.Model(p).Association("Tags").Replace(tags); err != nil {
			return errors.Wrap(err, "记录文章-标签关系失败")
		}
		return nil
	})
}
func (s *Store) DeletePost(ctx context.Context, id uint) error {
	defer s.InvalidateCache()
	return s.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("post_id = ?", id).Delete(&model.Comment{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM post_categories WHERE post_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM post_tags WHERE post_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Where("post_id = ?", id).Delete(&model.PostRevision{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Post{}, id).Error
	})
}
func (s *Store) AdminPostsByIDs(ctx context.Context, ids []uint) (map[uint]model.Post, error) {
	result := make(map[uint]model.Post, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	uniq := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return result, nil
	}
	var posts []model.Post
	err := s.DB(ctx).Select("id", "title", "slug", "post_type", "published_at", "author_id").
		Preload("Categories").Preload("Author").
		Where("id IN ?", uniq).Find(&posts).Error
	if err != nil {
		return nil, errors.Wrap(err, "admin posts by ids")
	}
	for _, post := range posts {
		result[post.ID] = post
	}
	return result, nil
}
func (s *Store) ListPosts(ctx context.Context, page, pageSize int, categorySlug, tagSlug string) (*ListPostsResult, error) {
	if page < 1 {
		page = 1
	}
	q := s.DB(ctx).Omit("CommentCount").Model(&model.Post{}).
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
	s.fillCommentCounts(ctx, posts)

	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	return &ListPostsResult{Posts: posts, Total: total, Page: page, Pages: pages}, nil
}
func (s *Store) SearchPosts(ctx context.Context, keyword string, page, pageSize int) (*ListPostsResult, error) {
	if page < 1 {
		page = 1
	}
	like := "%" + keyword + "%"
	q := s.DB(ctx).Omit("CommentCount").Model(&model.Post{}).
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
	s.fillCommentCounts(ctx, posts)

	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	return &ListPostsResult{Posts: posts, Total: total, Page: page, Pages: pages}, nil
}
func (s *Store) ApprovedCommentCounts(ctx context.Context, postIDs []uint) map[uint]int64 {
	out := map[uint]int64{}
	if len(postIDs) == 0 {
		return out
	}
	type row struct {
		PostID uint
		N      int64
	}
	var rows []row
	s.DB(ctx).Model(&model.Comment{}).
		Select("post_id, COUNT(*) AS n").
		Where("post_id IN ? AND status = ?", postIDs, model.CommentApproved).
		Group("post_id").Scan(&rows)
	for _, r := range rows {
		out[r.PostID] = r.N
	}
	return out
}
func (s *Store) GetPostByID(ctx context.Context, id uint) (*model.Post, error) {
	var p model.Post
	err := s.DB(ctx).
		Omit("CommentCount").
		Preload("Categories").
		Preload("Tags").
		Preload("Author").
		Where("id = ? AND status = ?", id, model.StatusPublished).
		First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}
func (s *Store) ResolvePostByPath(ctx context.Context, path string, match *permalink.PostPathMatch) (*model.Post, error) {
	if match == nil {
		return nil, gorm.ErrRecordNotFound
	}
	q := s.DB(ctx).Omit("CommentCount").Model(&model.Post{}).
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
func (s *Store) GetPostAnyStatus(ctx context.Context, id uint) (*model.Post, error) {
	var p model.Post
	err := s.DB(ctx).Omit("CommentCount").Preload("Categories").Preload("Tags").Preload("Author").
		Where("id = ? AND post_type = ?", id, model.PostTypePost).
		First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}
func (s *Store) PrevPost(ctx context.Context, t time.Time) *model.Post {
	var p model.Post
	err := s.DB(ctx).Select("id", "title", "slug", "published_at", "author_id").
		Where("post_type = ? AND status = ? AND published_at < ?",
			model.PostTypePost, model.StatusPublished, t).
		Order("published_at DESC").First(&p).Error
	if err != nil {
		return nil
	}
	return &p
}
func (s *Store) NextPost(ctx context.Context, t time.Time) *model.Post {
	var p model.Post
	err := s.DB(ctx).Select("id", "title", "slug", "published_at", "author_id").
		Where("post_type = ? AND status = ? AND published_at > ?",
			model.PostTypePost, model.StatusPublished, t).
		Order("published_at ASC").First(&p).Error
	if err != nil {
		return nil
	}
	return &p
}
func (s *Store) GetPageBySlug(ctx context.Context, slug string) (*model.Post, error) {
	var p model.Post
	err := s.DB(ctx).Omit("CommentCount").Preload("Author").Where("slug = ? AND post_type = ? AND status = ?",
		slug, model.PostTypePage, model.StatusPublished).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}
func (s *Store) IncrementViews(ctx context.Context, id uint) error {
	return s.DB(ctx).
		Model(&model.Post{}).
		Where("id = ?", id).
		UpdateColumn("views", gorm.Expr("views + 1")).Error
}
func (s *Store) AllPostsForArchive(ctx context.Context) ([]model.Post, error) {
	var posts []model.Post
	err := s.DB(ctx).Select("id", "title", "slug", "published_at", "author_id").
		Where("post_type = ? AND status = ?", model.PostTypePost, model.StatusPublished).
		Order("published_at DESC").Find(&posts).Error
	if err != nil {
		return nil, errors.Wrap(err, "archive posts")
	}
	return posts, nil
}
func (s *Store) MenuPages(ctx context.Context) ([]model.Post, error) {
	var pages []model.Post
	err := s.DB(ctx).Where("post_type = ? AND status = ? AND menu_order > 0",
		model.PostTypePage, model.StatusPublished).
		Order("menu_order ASC").Find(&pages).Error
	if err != nil {
		return nil, errors.Wrap(err, "menu pages")
	}
	return pages, nil
}

func (s *Store) PostMeta(ctx context.Context, id uint) (*model.Post, error) {
	var p model.Post
	err := s.DB(ctx).
		Select("id", "title", "published_at", "status", "post_type", "slug", "comment_status", "author_id").
		Preload("Categories").
		Preload("Author").
		Where("id = ?", id).
		First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PostMetas 批量查询文章元数据，避免 N+1。
func (s *Store) PostMetas(ctx context.Context, ids []uint) (map[uint]*model.Post, error) {
	if len(ids) == 0 {
		return map[uint]*model.Post{}, nil
	}
	var posts []model.Post
	err := s.DB(ctx).Select("id", "title", "published_at", "status", "post_type", "slug", "comment_status", "author_id").
		Preload("Categories").Preload("Author").
		Where("id IN ?", ids).Find(&posts).Error
	if err != nil {
		return nil, err
	}
	m := make(map[uint]*model.Post, len(posts))
	for i := range posts {
		m[posts[i].ID] = &posts[i]
	}
	return m, nil
}
func (s *Store) RecentPosts(ctx context.Context, n int) []model.Post {
	var ps []model.Post
	s.DB(ctx).Select("id", "title", "slug", "published_at", "author_id").
		Preload("Categories").Preload("Author").
		Where("post_type = ? AND status = ?", model.PostTypePost, model.StatusPublished).
		Order("published_at DESC").Limit(n).Find(&ps)
	return ps
}

// PublishScheduled 将已到发布时间的定时文章改为已发布状态，返回受影响行数。
func (s *Store) PublishScheduled(ctx context.Context) (int64, error) {
	result := s.DB(ctx).Model(&model.Post{}).
		Where("status = ? AND published_at <= ?", model.StatusScheduled, time.Now()).
		Update("status", model.StatusPublished)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected > 0 {
		s.InvalidateCache()
	}
	return result.RowsAffected, nil
}

func (s *Store) ArchiveMonths(ctx context.Context) []ArchiveMonth {
	type row struct {
		Ym string
		N  int64
	}
	var rows []row
	s.DB(ctx).Model(&model.Post{}).
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
