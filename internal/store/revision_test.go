package store

import (
	"context"
	"testing"

	"github.com/youthlin/wenlog/internal/model"
)

func TestCreateRevisionTracksCustomCSS(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	post := &model.Post{
		ID:            1,
		Title:         "Post",
		ContentMD:     "body",
		Content:       "<p>body</p>",
		CustomCSS:     ".cols { width: 33%; }",
		PostType:      model.PostTypePost,
		Status:        model.StatusPublished,
		ContentFormat: model.FormatMarkdown,
	}

	if err := st.CreateRevision(ctx, post); err != nil {
		t.Fatalf("create first revision: %v", err)
	}
	if err := st.CreateRevision(ctx, post); err != nil {
		t.Fatalf("create unchanged revision: %v", err)
	}
	revs, err := st.ListRevisions(ctx, post.ID)
	if err != nil {
		t.Fatalf("list revisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("unchanged custom css should not create a duplicate revision, got %d", len(revs))
	}
	if revs[0].CustomCSS != post.CustomCSS {
		t.Fatalf("revision custom css = %q, want %q", revs[0].CustomCSS, post.CustomCSS)
	}

	post.CustomCSS = ".cols { width: 50%; }"
	if err := st.CreateRevision(ctx, post); err != nil {
		t.Fatalf("create css-changed revision: %v", err)
	}
	revs, err = st.ListRevisions(ctx, post.ID)
	if err != nil {
		t.Fatalf("list revisions after css change: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("custom css change should create a new revision, got %d", len(revs))
	}
	if revs[0].CustomCSS != post.CustomCSS {
		t.Fatalf("latest revision custom css = %q, want %q", revs[0].CustomCSS, post.CustomCSS)
	}
}
