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
	// 同步更新缓存中的 Views 字段（指针修改，后续读缓存可见）
	if s.cache != nil {
		if p, ok := s.cache.Posts[id]; ok {
			p.Views++
		}
	}
	s.viewsMu.Unlock()
	return nil
}

// FlushViews 将内存中的浏览量增量批量写入数据库，并清空内存计数。
func (s *Store) FlushViews(ctx context.Context) error {
	s.viewsMu.Lock()
	counts := s.views.counts
	s.views.counts = make(map[uint]int64)
	s.viewsMu.Unlock()

	if len(counts) == 0 {
		return nil
	}

	for id, delta := range counts {
		if err := s.gormDB.WithContext(ctx).
			Model(&model.Post{}).
			Where("id = ?", id).
			UpdateColumn("views", gorm.Expr("views + ?", delta)).Error; err != nil {
			slog.WarnContext(ctx, "批量更新浏览量失败", "error", err, "post_id", id, "delta", delta)
		}
	}
	return nil
}
