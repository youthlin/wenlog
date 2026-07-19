package handler

import (
	"context"
	"testing"

	"github.com/youthlin/wenlog/internal/model"
)

func TestNormalizeCommentStatus(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		in   string
		want string
	}{
		{model.CommentApproved, model.CommentApproved},
		{model.CommentPending, model.CommentPending},
		{model.CommentSpam, model.CommentSpam},
		{"reject", model.CommentPending},
		{"blocked", model.CommentPending},
		{"", model.CommentPending},
		{"unknown", model.CommentPending},
	}
	for _, tc := range cases {
		got := normalizeCommentStatus(ctx, tc.in)
		if got != tc.want {
			t.Errorf("normalizeCommentStatus(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
