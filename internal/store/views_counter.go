package store

import (
	"context"
	"log/slog"
	"sync"

	"gorm.io/gorm"

	"github.com/youthlin/wenlog/internal/model"
)

type viewsCounter struct {
	mu     sync.Mutex
	counts map[uint]int64
}

func newViewsCounter() *viewsCounter {
	return &viewsCounter{counts: make(map[uint]int64)}
}

// IncrementViews 在内存中为指定文章/页面浏览量 +1，不立即写入数据库。
// 同时更新缓存中 Post.Views 字段，使当前请求及后续请求能实时看到浏览量。
// 由后台定时任务批量 Flush 到数据库。
func (s *Store) IncrementViews(ctx context.Context, id uint) error {
	s.viewsMu.Lock()
	s.views.counts[id]++
	if s.cache != nil {
		if p, ok := s.cache.Posts[id]; ok {
			p.Views++
		}
	}
	s.viewsMu.Unlock()
	return nil
}

// FlushViews 将内存中的浏览量增量批量写入数据库。
// 单条写入成功后才从内存移除对应计数；写入失败的保留在内存中，下次 flush 重试。
func (s *Store) FlushViews(ctx context.Context) error {
	s.viewsMu.Lock()
	if len(s.views.counts) == 0 {
		s.viewsMu.Unlock()
		return nil
	}
	// 快照当前计数，并清空内存（新请求的增量写到新 map 中）
	snapshot := s.views.counts
	s.views.counts = make(map[uint]int64)
	s.viewsMu.Unlock()

	var firstErr error
	failed := make(map[uint]int64)
	for id, delta := range snapshot {
		if err := s.gormDB.WithContext(ctx).
			Model(&model.Post{}).
			Where("id = ?", id).
			UpdateColumn("views", gorm.Expr("views + ?", delta)).Error; err != nil {
			slog.WarnContext(ctx, "批量更新浏览量失败，将在下次重试", "error", err, "post_id", id, "delta", delta)
			if firstErr == nil {
				firstErr = err
			}
			failed[id] = delta
		}
	}
	// 将失败的条目合并回内存计数，避免丢失
	if len(failed) > 0 {
		s.viewsMu.Lock()
		for id, delta := range failed {
			s.views.counts[id] += delta
		}
		s.viewsMu.Unlock()
	}
	return firstErr
}
