package store

import (
	"context"
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
	if post.Views != 6 {
		t.Errorf("cached views=%d, want 6", post.Views)
	}
}
