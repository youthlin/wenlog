package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/youthlin/wenlog/internal/model"
)

func TestFlushViews_Success(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	db := st.DB(ctx)
	now := time.Now()
	p := model.Post{ID: 1, Title: "p1", PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: now, Views: 10}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	// Increment 3 times
	for i := 0; i < 3; i++ {
		st.IncrementViews(ctx, 1)
	}
	// Before flush: memory should have 3
	st.viewsMu.Lock()
	if st.views.counts[1] != 3 {
		t.Fatalf("before flush counts[1]=%d, want 3", st.views.counts[1])
	}
	st.viewsMu.Unlock()

	if err := st.FlushViews(ctx); err != nil {
		t.Fatalf("FlushViews: %v", err)
	}
	// After flush: memory cleared
	st.viewsMu.Lock()
	if st.views.counts[1] != 0 {
		t.Fatalf("after flush counts[1]=%d, want 0", st.views.counts[1])
	}
	st.viewsMu.Unlock()
	// DB should have views=13
	var got model.Post
	if err := db.First(&got, 1).Error; err != nil {
		t.Fatal(err)
	}
	if got.Views != 13 {
		t.Errorf("DB views=%d, want 13", got.Views)
	}
}

func TestFlushViews_FailurePreservesDeltas(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	db := st.DB(ctx)
	now := time.Now()
	p := model.Post{ID: 1, Title: "p1", PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: now, Views: 0}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	st.IncrementViews(ctx, 1)
	st.IncrementViews(ctx, 1)

	// Close underlying sql.DB to force write failure
	sqlDB, err := st.gormDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	ferr := st.FlushViews(ctx)
	if ferr == nil {
		t.Fatal("FlushViews should return error after DB close")
	}
	// Deltas should be preserved in memory for retry
	st.viewsMu.Lock()
	delta := st.views.counts[1]
	st.viewsMu.Unlock()
	if delta != 2 {
		t.Errorf("after failed flush counts[1]=%d, want 2 (delta preserved)", delta)
	}
}

func TestIncrementViews_UpdatesCache(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// Manually populate cache with a post
	post := &model.Post{ID: 1, Views: 5}
	st.cacheMu.Lock()
	st.cache = &DataLoader{Posts: map[uint]*model.Post{1: post}}
	st.cacheMu.Unlock()

	st.IncrementViews(ctx, 1)
	if post.Views != 5 {
		t.Errorf("original cached post views=%d, want unchanged 5", post.Views)
	}
	st.cacheMu.RLock()
	got := st.cache.Posts[1]
	st.cacheMu.RUnlock()
	if got == post {
		t.Fatal("IncrementViews should replace cached post instead of mutating shared post in place")
	}
	if got.Views != 6 {
		t.Errorf("new cached views=%d, want 6", got.Views)
	}
}

func TestIncrementViews_CopyOnWriteCacheDoesNotMutateExistingLoader(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	post := &model.Post{ID: 1, Slug: "p1", PostType: model.PostTypePost, Status: model.StatusPublished, Views: 5}
	loader := &DataLoader{
		Posts:          map[uint]*model.Post{1: post},
		postsBySlug:    map[string]*model.Post{"p1": post},
		postsByType:    map[string][]*model.Post{model.PostTypePost: []*model.Post{post}},
		menuPages:      []*model.Post{post},
		Categories:     map[uint]*model.Category{},
		Tags:           map[uint]*model.Tag{},
		Users:          map[uint]*model.User{},
		Settings:       map[string]string{},
		Comments:       map[uint]*model.Comment{},
		commentsByPost: map[uint][]uint{},
	}
	st.cacheMu.Lock()
	st.cache = loader
	st.cacheMu.Unlock()

	st.IncrementViews(ctx, 1)

	if post.Views != 5 {
		t.Fatalf("old loader post was mutated in place: views=%d, want 5", post.Views)
	}
	st.cacheMu.RLock()
	newLoader := st.cache
	newPost := newLoader.Posts[1]
	st.cacheMu.RUnlock()
	if newLoader == loader {
		t.Fatal("cache loader should be replaced copy-on-write")
	}
	if newPost == post {
		t.Fatal("cache post should be replaced copy-on-write")
	}
	if newPost.Views != 6 {
		t.Fatalf("new cached post views=%d, want 6", newPost.Views)
	}
	if newLoader.postsBySlug["p1"] != newPost {
		t.Fatal("postsBySlug should point to the copied post")
	}
	if newLoader.postsByType[model.PostTypePost][0] != newPost {
		t.Fatal("postsByType should point to the copied post")
	}
	if newLoader.menuPages[0] != newPost {
		t.Fatal("menuPages should point to the copied post")
	}
}

func TestIncrementViews_ConcurrentCacheReaders(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	post := &model.Post{ID: 1, Views: 5}
	st.cacheMu.Lock()
	st.cache = &DataLoader{Posts: map[uint]*model.Post{1: post}}
	st.cacheMu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				st.cacheMu.RLock()
				loader := st.cache
				st.cacheMu.RUnlock()
				if loader != nil && loader.Posts != nil {
					if p := loader.Posts[1]; p != nil {
						_ = p.Views
					}
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if err := st.IncrementViews(ctx, 1); err != nil {
					t.Errorf("IncrementViews: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}
