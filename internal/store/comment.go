// Package store — 评论相关方法。
package store

import (
	"context"
	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/model"
)

func (s *Store) AdminListComments(ctx context.Context, status string, postID uint, page, pageSize int) ([]model.Comment, int64, error) {
	return s.AdminListCommentsForAuthor(ctx, status, postID, page, pageSize, 0)
}
func (s *Store) AdminListCommentsForAuthor(ctx context.Context, status string, postID uint, page, pageSize int, authorID uint) ([]model.Comment, int64, error) {
	q := s.db.Model(&model.Comment{})
	if authorID > 0 {
		q = q.Joins("JOIN posts ON posts.id = comments.post_id").Where("posts.author_id = ?", authorID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if postID > 0 {
		q = q.Where("post_id = ?", postID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count comments")
	}
	var comments []model.Comment
	err := q.Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&comments).Error
	return comments, total, errors.Wrap(err, "admin list comments")
}
func (s *Store) SetCommentStatus(ctx context.Context, id uint, status string) error {
	return errors.Wrap(
		s.db.Model(&model.Comment{}).Where("id = ?", id).Update("status", status).Error,
		"set comment status")
}
func (s *Store) DeleteComment(ctx context.Context, id uint) error {
	return s.softDeleteComments(ctx, []uint{id})
}
func (s *Store) UpdateCommentFields(ctx context.Context, id uint, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return errors.Wrap(
		s.db.Model(&model.Comment{}).Where("id = ?", id).Updates(fields).Error,
		"update comment fields")
}
func (s *Store) BatchSetCommentStatus(ctx context.Context, ids []uint, status string) error {
	if len(ids) == 0 {
		return nil
	}
	return errors.Wrap(
		s.db.Model(&model.Comment{}).Where("id IN ?", ids).Update("status", status).Error,
		"batch set comment status")
}
func (s *Store) BatchDeleteComments(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return s.softDeleteComments(ctx, ids)
}
func (s *Store) ListCommentsByUser(ctx context.Context, userID uint, page, pageSize int) ([]model.Comment, int64, error) {
	q := s.db.Model(&model.Comment{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count user comments")
	}
	var comments []model.Comment
	err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&comments).Error
	return comments, total, errors.Wrap(err, "list user comments")
}
func (s *Store) DeleteCommentByUser(ctx context.Context, commentID, userID uint) error {
	result := s.db.Model(&model.Comment{}).
		Where("id = ? AND user_id = ?", commentID, userID).
		Update("status", model.CommentDeleted)
	if result.RowsAffected == 0 {
		return errors.New("comment not found or not owned by user")
	}
	return errors.Wrap(result.Error, "delete user comment")
}
func (s *Store) ApprovedComments(ctx context.Context, postID uint) ([]model.Comment, error) {
	var comments []model.Comment
	err := s.db.Where("post_id = ? AND status = ?", postID, model.CommentApproved).
		Order("created_at ASC").Find(&comments).Error
	if err != nil {
		return nil, errors.Wrap(err, "approved comments")
	}
	return comments, nil
}
func (s *Store) ApprovedCommentsPage(ctx context.Context, postID uint, page, pageSize int) (*CommentPageResult, error) {
	return s.VisibleCommentsPageForViewer(ctx, postID, page, pageSize, 0, nil)
}
func (s *Store) VisibleCommentsPage(ctx context.Context, postID uint, page, pageSize int, currentUserID uint) (*CommentPageResult, error) {
	return s.VisibleCommentsPageForViewer(ctx, postID, page, pageSize, currentUserID, nil)
}
func (s *Store) VisibleCommentsPageForViewer(ctx context.Context, postID uint, page, pageSize int, currentUserID uint, pendingCommentIDs []uint) (*CommentPageResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	var totalAll int64
	if err := visibleCommentsQuery(s.db.Model(&model.Comment{}), postID, currentUserID, pendingCommentIDs).
		Count(&totalAll).Error; err != nil {
		return nil, errors.Wrap(err, "count visible comments")
	}

	qTop := visibleCommentsQuery(s.db.Model(&model.Comment{}), postID, currentUserID, pendingCommentIDs).
		Where("parent_id = 0")
	var totalTop int64
	if err := qTop.Count(&totalTop).Error; err != nil {
		return nil, errors.Wrap(err, "count top comments")
	}
	pages := int((totalTop + int64(pageSize) - 1) / int64(pageSize))
	if pages == 0 {
		pages = 1
	}
	if page > pages {
		page = pages
	}

	var tops []model.Comment
	err := qTop.Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&tops).Error
	if err != nil {
		return nil, errors.Wrap(err, "list top comments")
	}
	if len(tops) == 0 {
		return &CommentPageResult{Page: page, Pages: pages, TotalTop: totalTop, TotalComments: totalAll}, nil
	}

	ids := make([]uint, len(tops))
	index := make(map[uint][]model.Comment, len(tops))
	for i := range tops {
		ids[i] = tops[i].ID
	}
	var children []model.Comment
	if err := visibleCommentsQuery(s.db.Model(&model.Comment{}), postID, currentUserID, pendingCommentIDs).
		Where("parent_id IN ?", ids).
		Order("created_at ASC").Find(&children).Error; err != nil {
		return nil, errors.Wrap(err, "list child comments")
	}
	for _, c := range children {
		index[c.ParentID] = append(index[c.ParentID], c)
	}

	out := make([]model.Comment, 0, len(tops)+len(children))
	for _, top := range tops {
		out = append(out, top)
		out = append(out, index[top.ID]...)
	}
	populateReplyToAuthors(s.db, out)
	populateCommenterRoles(s.db, out, postID)
	return &CommentPageResult{Comments: out, TotalComments: totalAll, TotalTop: totalTop, Page: page, Pages: pages}, nil
}
func (s *Store) CommentPageForID(ctx context.Context, commentID uint, pageSize int) int {
	return s.VisibleCommentPageForViewerID(ctx, commentID, pageSize, 0, nil)
}
func (s *Store) VisibleCommentPageForID(ctx context.Context, commentID uint, pageSize int, currentUserID uint) int {
	return s.VisibleCommentPageForViewerID(ctx, commentID, pageSize, currentUserID, nil)
}
func (s *Store) VisibleCommentPageForViewerID(ctx context.Context, commentID uint, pageSize int, currentUserID uint, pendingCommentIDs []uint) int {
	if pageSize < 1 {
		pageSize = 20
	}
	var c model.Comment
	if err := s.db.Select("id", "post_id", "parent_id", "created_at", "status", "user_id").
		First(&c, commentID).
		Error; err != nil {
		return 1
	}
	if c.Status != model.CommentApproved && !commentVisibleToViewer(c, currentUserID, pendingCommentIDs) {
		return 1
	}
	rootID := c.ID
	rootCreatedAt := c.CreatedAt
	if c.ParentID != 0 {
		var parent model.Comment
		if err := s.db.Select("id", "created_at", "status", "user_id").
			First(&parent, c.ParentID).
			Error; err != nil {
			return 1
		}
		if parent.Status != model.CommentApproved && !commentVisibleToViewer(parent, currentUserID, pendingCommentIDs) {
			return 1
		}
		rootID = parent.ID
		rootCreatedAt = parent.CreatedAt
	}
	var newer int64
	_ = visibleCommentsQuery(s.db.Model(&model.Comment{}), c.PostID, currentUserID, pendingCommentIDs).
		Where("parent_id = 0").
		Where("created_at > ? OR (created_at = ? AND id > ?)", rootCreatedAt, rootCreatedAt, rootID).
		Count(&newer).Error
	return int(newer)/pageSize + 1
}
func (s *Store) ResolveCommentReply(ctx context.Context, postID uint, replyToID uint, currentUserID uint, pendingCommentIDs []uint) (uint, uint, error) {
	if replyToID == 0 {
		return 0, 0, nil
	}
	var target model.Comment
	if err := s.db.Select("id", "post_id", "parent_id", "status", "user_id").First(&target, replyToID).Error; err != nil {
		return 0, 0, errors.Wrap(err, "get reply target")
	}
	if target.PostID != postID || (target.Status != model.CommentApproved && !commentVisibleToViewer(target, currentUserID, pendingCommentIDs)) {
		return 0, 0, errors.New("reply target not visible")
	}
	if target.ParentID == 0 {
		return target.ID, target.ID, nil
	}
	var parent model.Comment
	if err := s.db.Select("id", "post_id", "status", "user_id").First(&parent, target.ParentID).Error; err != nil {
		return 0, 0, errors.Wrap(err, "get reply parent")
	}
	if parent.PostID != postID || (parent.Status != model.CommentApproved && !commentVisibleToViewer(parent, currentUserID, pendingCommentIDs)) {
		return 0, 0, errors.New("reply parent not visible")
	}
	return parent.ID, target.ID, nil
}
func (s *Store) CreateComment(ctx context.Context, c *model.Comment) error {
	return errors.Wrap(s.db.Create(c).Error, "create comment")
}
func (s *Store) GetCommentByID(ctx context.Context, id uint) (*model.Comment, error) {
	var c model.Comment
	if err := s.db.First(&c, id).Error; err != nil {
		return nil, errors.Wrap(err, "get comment by id")
	}
	return &c, nil
}
func (s *Store) CommentsByIDs(ctx context.Context, ids []uint) ([]model.Comment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var comments []model.Comment
	if err := s.db.Where("id IN ?", ids).Find(&comments).Error; err != nil {
		return nil, errors.Wrap(err, "comments by ids")
	}
	return comments, nil
}
func (s *Store) RecentCommentCountByIP(ctx context.Context, ip string, sinceUnix int64) (int64, error) {
	var n int64
	err := s.db.Model(&model.Comment{}).
		Where("ip = ? AND created_at > datetime(?, 'unixepoch')", ip, sinceUnix).
		Count(&n).Error
	return n, errors.Wrap(err, "recent comment count")
}
func (s *Store) RecentComments(ctx context.Context, n int) []model.Comment {
	var cs []model.Comment
	s.db.Where("status = ?", model.CommentApproved).
		Order("created_at DESC").Limit(n).Find(&cs)
	return cs
}
func (s *Store) RecentCommentItems(ctx context.Context, n, pageSize int) []CommentWidgetItem {
	comments := s.RecentComments(ctx, n)
	return s.buildCommentWidgetItems(ctx, comments, pageSize)
}
func (s *Store) SayingComments(ctx context.Context, postID uint, n int) []model.Comment {
	var cs []model.Comment
	s.db.Where("post_id = ? AND status = ? AND email IN (?)",
		postID, model.CommentApproved,
		s.db.Model(&model.User{}).Select("email")).
		Order("created_at DESC").Limit(n).Find(&cs)
	return cs
}
func (s *Store) SayingCommentItems(ctx context.Context, postID uint, n, pageSize int) []CommentWidgetItem {
	comments := s.SayingComments(ctx, postID, n)
	return s.buildCommentWidgetItems(ctx, comments, pageSize)
}
