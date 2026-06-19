// Package store — 标签相关方法。
package store

import (
	"context"
	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/model"
	"gorm.io/gorm"
	"strings"
)

func (s *Store) TagSlugExists(ctx context.Context, slug string, excludeID uint) (bool, error) {
	var n int64
	q := s.db(ctx).Model(&model.Tag{}).Where("slug = ?", slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count tag slug")
}
func (s *Store) SaveTag(ctx context.Context, tag *model.Tag) error {
	defer s.InvalidateCache()
	return errors.Wrap(s.db(ctx).Save(tag).Error, "save tag")
}
func (s *Store) DeleteTag(ctx context.Context, id uint) error {
	defer s.InvalidateCache()
	return s.db(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM post_tags WHERE tag_id = ?", id).Error; err != nil {
			return errors.Wrap(err, "delete tag relations")
		}
		if err := tx.Delete(&model.Tag{}, id).Error; err != nil {
			return errors.Wrap(err, "delete tag")
		}
		return nil
	})
}
func (s *Store) AdminListTags(ctx context.Context, keyword string, page, pageSize int) ([]model.Tag, int64, error) {
	q := s.db(ctx).Model(&model.Tag{})
	applyKeyword := func(db *gorm.DB) *gorm.DB {
		if kw := strings.TrimSpace(keyword); kw != "" {
			like := termQueryLike(kw)
			return db.Where("LOWER(tags.name) LIKE ? OR LOWER(tags.slug) LIKE ?", like, like)
		}
		return db
	}
	q = applyKeyword(q)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count tags")
	}
	var tags []model.Tag
	listQ := applyKeyword(s.db(ctx).Model(&model.Tag{}))
	err := listQ.Select("tags.*, COUNT(DISTINCT posts.id) AS post_count").
		Joins("LEFT JOIN post_tags pt_count ON pt_count.tag_id = tags.id").
		Joins("LEFT JOIN posts ON posts.id = pt_count.post_id AND posts.post_type = ?", model.PostTypePost).
		Group("tags.id").Order("tags.name ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&tags).Error
	return tags, total, errors.Wrap(err, "admin list tags")
}
func (s *Store) AllTags(ctx context.Context) []model.Tag {
	var ts []model.Tag
	s.db(ctx).Order("name ASC").Find(&ts)
	return ts
}
