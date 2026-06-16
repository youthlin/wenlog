// Package store — 分类相关方法。
package store

import (
	"context"
	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/model"
	"gorm.io/gorm"
	"strings"
)

func (s *Store) CategorySlugExists(ctx context.Context, slug string, excludeID uint) (bool, error) {
	var n int64
	q := s.db.Model(&model.Category{}).Where("slug = ?", slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count category slug")
}
func (s *Store) SaveCategory(ctx context.Context, cat *model.Category) error {
	return errors.Wrap(s.db.Save(cat).Error, "save category")
}
func (s *Store) DeleteCategory(ctx context.Context, id uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var cat model.Category
		if err := tx.First(&cat, id).Error; err != nil {
			return errors.Wrap(err, "load category")
		}
		if cat.Slug == "uncategorized" {
			return ErrCannotDeleteUncategorized
		}

		fallbackID := cat.ParentID
		if fallbackID > 0 {
			var n int64
			if err := tx.Model(&model.Category{}).Where("id = ?", fallbackID).Count(&n).Error; err != nil {
				return errors.Wrap(err, "count parent category")
			}
			if n == 0 {
				fallbackID = 0
			}
		}
		if fallbackID == 0 {
			fallback, err := ensureUncategorizedCategory(tx)
			if err != nil {
				return err
			}
			fallbackID = fallback.ID
		}

		if fallbackID != id {
			if err := tx.Exec(`
				INSERT INTO post_categories (post_id, category_id)
				SELECT pc.post_id, ?
				FROM post_categories pc
				WHERE pc.category_id = ?
				  AND NOT EXISTS (
				    SELECT 1 FROM post_categories existing
				    WHERE existing.post_id = pc.post_id AND existing.category_id = ?
				  )`, fallbackID, id, fallbackID).Error; err != nil {
				return errors.Wrap(err, "move category relations")
			}
		}
		if err := tx.Model(&model.Category{}).Where("parent_id = ?", id).Update("parent_id", fallbackID).Error; err != nil {
			return errors.Wrap(err, "detach child categories")
		}
		if err := tx.Exec("DELETE FROM post_categories WHERE category_id = ?", id).Error; err != nil {
			return errors.Wrap(err, "delete category relations")
		}
		if err := tx.Delete(&model.Category{}, id).Error; err != nil {
			return errors.Wrap(err, "delete category")
		}
		return nil
	})
}
func (s *Store) AdminListCategories(ctx context.Context, keyword string, page, pageSize int) ([]model.Category, int64, error) {
	q := s.db.Model(&model.Category{})
	applyKeyword := func(db *gorm.DB) *gorm.DB {
		if kw := strings.TrimSpace(keyword); kw != "" {
			like := termQueryLike(kw)
			return db.Where("LOWER(categories.name) LIKE ? OR LOWER(categories.slug) LIKE ? OR LOWER(categories.description) LIKE ?", like, like, like)
		}
		return db
	}
	q = applyKeyword(q)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count categories")
	}
	var categories []model.Category
	listQ := applyKeyword(s.db.Model(&model.Category{}))
	err := listQ.Select("categories.*, COUNT(DISTINCT posts.id) AS post_count").
		Joins("LEFT JOIN post_categories pc_count ON pc_count.category_id = categories.id").
		Joins("LEFT JOIN posts ON posts.id = pc_count.post_id AND posts.post_type = ?", model.PostTypePost).
		Group("categories.id").Order("categories.name ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&categories).Error
	return categories, total, errors.Wrap(err, "admin list categories")
}
func (s *Store) AllCategories(ctx context.Context) []model.Category {
	var cs []model.Category
	s.db.Order("name ASC").Find(&cs)
	return cs
}
