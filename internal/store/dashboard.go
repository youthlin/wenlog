// Package store — 仪表盘统计相关方法。
package store

import (
	"context"
	"github.com/youthlin/blog/internal/model"
)

func (s *Store) DashboardStats(ctx context.Context) DashboardStats {
	var ds DashboardStats
	s.db(ctx).Model(&model.Post{}).Where("post_type = ? AND status = ?", model.PostTypePost, model.StatusPublished).Count(&ds.PostsPublished)
	s.db(ctx).Model(&model.Post{}).Where("post_type = ? AND status = ?", model.PostTypePost, model.StatusDraft).Count(&ds.PostsDraft)
	s.db(ctx).Model(&model.Post{}).Where("post_type = ? AND status = ?", model.PostTypePage, model.StatusPublished).Count(&ds.PagesPublished)
	s.db(ctx).Model(&model.Post{}).Where("post_type = ? AND status = ?", model.PostTypePage, model.StatusDraft).Count(&ds.PagesDraft)
	ds.Comments = s.AdminCommentCounts(ctx)
	s.db(ctx).Model(&model.Category{}).Count(&ds.CategoryCount)
	s.db(ctx).Model(&model.Tag{}).Count(&ds.TagCount)
	s.db(ctx).Model(&model.Upload{}).Count(&ds.UploadCount)
	return ds
}
func (s *Store) AuthorDashboardStats(ctx context.Context, authorID uint) DashboardStats {
	var ds DashboardStats
	s.db(ctx).Model(&model.Post{}).Where("post_type = ? AND status = ? AND author_id = ?", model.PostTypePost, model.StatusPublished, authorID).Count(&ds.PostsPublished)
	s.db(ctx).Model(&model.Post{}).Where("post_type = ? AND status = ? AND author_id = ?", model.PostTypePost, model.StatusDraft, authorID).Count(&ds.PostsDraft)
	s.db(ctx).Model(&model.Post{}).Where("post_type = ? AND status = ? AND author_id = ?", model.PostTypePage, model.StatusPublished, authorID).Count(&ds.PagesPublished)
	s.db(ctx).Model(&model.Post{}).Where("post_type = ? AND status = ? AND author_id = ?", model.PostTypePage, model.StatusDraft, authorID).Count(&ds.PagesDraft)
	return ds
}
func (s *Store) ReaderDashboardStats(ctx context.Context, userID uint) ReaderStats {
	var rs ReaderStats
	s.db(ctx).Model(&model.Comment{}).Where("user_id = ? AND status != ?", userID, model.CommentDeleted).Count(&rs.CommentCount)
	s.db(ctx).Model(&model.Comment{}).Where("user_id = ? AND status = ?", userID, model.CommentPending).Count(&rs.PendingCommentCount)
	s.db(ctx).Model(&model.Comment{}).Where("user_id = ? AND status != ?", userID, model.CommentDeleted).
		Joins("JOIN posts ON posts.id = comments.post_id AND posts.post_type = ?", model.PostTypePost).
		Distinct("comments.post_id").Count(&rs.CommentedPostCount)
	s.db(ctx).Model(&model.Comment{}).Where("user_id = ? AND status != ?", userID, model.CommentDeleted).
		Joins("JOIN posts ON posts.id = comments.post_id AND posts.post_type = ?", model.PostTypePage).
		Distinct("comments.post_id").Count(&rs.CommentedPageCount)
	return rs
}
type ctxKeyPendingCommentCount struct{}

func (s *Store) AdminCommentCounts(ctx context.Context) CommentCounts {
	var cc CommentCounts
	// 如果 ctx 中已有缓存的 pending 数，直接复用
	if cached, ok := ctx.Value(ctxKeyPendingCommentCount{}).(int64); ok {
		cc.Pending = cached
	} else {
		s.db(ctx).Model(&model.Comment{}).Where("status = ?", model.CommentPending).Count(&cc.Pending)
	}
	s.db(ctx).Model(&model.Comment{}).Where("status = ?", model.CommentApproved).Count(&cc.Approved)
	s.db(ctx).Model(&model.Comment{}).Where("status = ?", model.CommentSpam).Count(&cc.Spam)
	return cc
}
func (s *Store) PendingCommentCount(ctx context.Context) int64 {
	var n int64
	s.db(ctx).Model(&model.Comment{}).Where("status = ?", model.CommentPending).Count(&n)
	return n
}

// CtxWithPendingCommentCount 将 pending 评论数缓存到 context，供后续 AdminCommentCounts 复用。
func CtxWithPendingCommentCount(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, ctxKeyPendingCommentCount{}, n)
}
