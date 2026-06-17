// Package store — 上传相关方法。
package store

import (
	"context"
	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/model"
)

func (s *Store) SaveUpload(ctx context.Context, u *model.Upload) error {
	return errors.Wrap(s.db(ctx).Create(u).Error, "save upload")
}
func (s *Store) ListUploads(ctx context.Context, page, pageSize int) ([]model.Upload, int64, error) {
	return s.ListUploadsForUser(ctx, page, pageSize, 0)
}
func (s *Store) ListUploadsForUser(ctx context.Context, page, pageSize int, uploaderID uint) ([]model.Upload, int64, error) {
	q := s.db(ctx).Model(&model.Upload{})
	if uploaderID > 0 {
		q = q.Where("uploader_id = ?", uploaderID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count uploads")
	}
	var uploads []model.Upload
	err := q.Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&uploads).Error
	return uploads, total, errors.Wrap(err, "list uploads")
}
func (s *Store) GetUpload(ctx context.Context, id uint) (*model.Upload, error) {
	var u model.Upload
	if err := s.db(ctx).First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Store) DeleteUpload(ctx context.Context, id uint) error {
	return errors.Wrap(s.db(ctx).Delete(&model.Upload{}, id).Error, "delete upload")
}
