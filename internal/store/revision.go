package store

import (
	"context"

	"github.com/cockroachdb/errors"

	"github.com/youthlin/blog/internal/model"
)

// MaxRevisionsPerPost 每篇文章最多保留的修订版本数。
const MaxRevisionsPerPost = 20

// CreateRevision 为文章创建修订版本。如果内容与最新版本相同则跳过。
func (s *Store) CreateRevision(ctx context.Context, p *model.Post) error {
	// 检查是否与最新版本内容相同
	latest, err := s.LatestRevision(ctx, p.ID)
	if err == nil && latest != nil &&
		latest.Title == p.Title &&
		latest.ContentMD == p.ContentMD &&
		latest.Content == p.Content &&
		latest.Excerpt == p.Excerpt {
		return nil // 内容无变化，跳过
	}

	rev := &model.PostRevision{
		PostID:    p.ID,
		Title:     p.Title,
		ContentMD: p.ContentMD,
		Content:   p.Content,
		Excerpt:   p.Excerpt,
	}
	if err := s.DB(ctx).Create(rev).Error; err != nil {
		return errors.Wrap(err, "create revision")
	}

	// 清理超出上限的旧版本
	var count int64
	if err := s.DB(ctx).Model(&model.PostRevision{}).Where("post_id = ?", p.ID).Count(&count).Error; err != nil {
		return errors.Wrap(err, "count revisions")
	}
	if count > MaxRevisionsPerPost {
		// 保留最新的 MaxRevisionsPerPost 条，删除更旧的
		var ids []uint
		if err := s.DB(ctx).Model(&model.PostRevision{}).
			Where("post_id = ?", p.ID).
			Order("id DESC").
			Offset(MaxRevisionsPerPost).
			Limit(int(count)).
			Pluck("id", &ids).Error; err != nil {
			return errors.Wrap(err, "find old revisions")
		}
		if len(ids) > 0 {
			if err := s.DB(ctx).Where("id IN ?", ids).Delete(&model.PostRevision{}).Error; err != nil {
				return errors.Wrap(err, "delete old revisions")
			}
		}
	}
	return nil
}

// LatestRevision 获取文章的最新修订版本。
func (s *Store) LatestRevision(ctx context.Context, postID uint) (*model.PostRevision, error) {
	var rev model.PostRevision
	err := s.DB(ctx).Where("post_id = ?", postID).Order("id DESC").First(&rev).Error
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

// ListRevisions 获取文章的修订版本列表（按时间倒序）。
func (s *Store) ListRevisions(ctx context.Context, postID uint) ([]model.PostRevision, error) {
	var revs []model.PostRevision
	err := s.DB(ctx).Where("post_id = ?", postID).Order("id DESC").Find(&revs).Error
	if err != nil {
		return nil, errors.Wrap(err, "list revisions")
	}
	return revs, nil
}

// GetRevision 获取单个修订版本。
func (s *Store) GetRevision(ctx context.Context, id uint) (*model.PostRevision, error) {
	var rev model.PostRevision
	err := s.DB(ctx).First(&rev, id).Error
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

// DeleteRevisions 删除文章的所有修订版本。
func (s *Store) DeleteRevisions(ctx context.Context, postID uint) error {
	return s.DB(ctx).Where("post_id = ?", postID).Delete(&model.PostRevision{}).Error
}
