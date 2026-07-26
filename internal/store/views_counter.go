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
	s.viewsMu.Unlock()
	s.cacheMu.Lock()
	s.cache = cacheWithIncrementedViews(s.cache, id)
	s.cacheMu.Unlock()
	return nil
}

func cacheWithIncrementedViews(loader *DataLoader, id uint) *DataLoader {
	if loader == nil || loader.Posts == nil {
		return loader
	}
	oldPost := loader.Posts[id]
	if oldPost == nil {
		return loader
	}
	newPost := *oldPost
	newPost.Views++
	cloned := *loader
	cloned.Posts = replacePostMapEntry(loader.Posts, id, &newPost)
	cloned.postsBySlug = replacePostSlugMap(loader.postsBySlug, oldPost, &newPost)
	cloned.postsByType = replacePostTypeMap(loader.postsByType, oldPost, &newPost)
	cloned.menuPages = replacePostSlice(loader.menuPages, oldPost, &newPost)
	return &cloned
}

func replacePostMapEntry(posts map[uint]*model.Post, id uint, newPost *model.Post) map[uint]*model.Post {
	cloned := make(map[uint]*model.Post, len(posts))
	for key, post := range posts {
		cloned[key] = post
	}
	cloned[id] = newPost
	return cloned
}

func replacePostSlugMap(posts map[string]*model.Post, oldPost, newPost *model.Post) map[string]*model.Post {
	if posts == nil {
		return nil
	}
	cloned := make(map[string]*model.Post, len(posts))
	for key, post := range posts {
		if post == oldPost {
			post = newPost
		}
		cloned[key] = post
	}
	return cloned
}

func replacePostTypeMap(posts map[string][]*model.Post, oldPost, newPost *model.Post) map[string][]*model.Post {
	if posts == nil {
		return nil
	}
	cloned := make(map[string][]*model.Post, len(posts))
	for key, list := range posts {
		cloned[key] = replacePostSlice(list, oldPost, newPost)
	}
	return cloned
}

func replacePostSlice(list []*model.Post, oldPost, newPost *model.Post) []*model.Post {
	if list == nil {
		return nil
	}
	cloned := make([]*model.Post, len(list))
	for i, post := range list {
		if post == oldPost {
			post = newPost
		}
		cloned[i] = post
	}
	return cloned
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
