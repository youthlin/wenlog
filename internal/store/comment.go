// Package store — 评论相关方法。
package store

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/model"
)

func (s *Store) AdminListComments(ctx context.Context, status string, postID uint, page, pageSize int) ([]model.Comment, int64, error) {
	return s.AdminListCommentsForAuthor(ctx, status, postID, page, pageSize, 0)
}
func (s *Store) AdminListCommentsForAuthor(ctx context.Context, status string, postID uint, page, pageSize int, authorID uint) ([]model.Comment, int64, error) {
	q := s.DB(ctx).Model(&model.Comment{})
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
	defer s.InvalidateCache()
	return errors.Wrap(
		s.DB(ctx).Model(&model.Comment{}).Where("id = ?", id).Update("status", status).Error,
		"set comment status")
}
func (s *Store) DeleteComment(ctx context.Context, id uint) error {
	defer s.InvalidateCache()
	return s.softDeleteComments(ctx, []uint{id})
}
func (s *Store) UpdateCommentFields(ctx context.Context, id uint, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	if err := errors.Wrap(
		s.DB(ctx).Model(&model.Comment{}).Where("id = ?", id).Updates(fields).Error,
		"update comment fields"); err != nil {
		return err
	}
	s.InvalidateCache()
	return nil
}
func (s *Store) BatchSetCommentStatus(ctx context.Context, ids []uint, status string) error {
	defer s.InvalidateCache()
	if len(ids) == 0 {
		return nil
	}
	return errors.Wrap(
		s.DB(ctx).Model(&model.Comment{}).Where("id IN ?", ids).Update("status", status).Error,
		"batch set comment status")
}
func (s *Store) BatchDeleteComments(ctx context.Context, ids []uint) error {
	defer s.InvalidateCache()
	if len(ids) == 0 {
		return nil
	}
	return s.softDeleteComments(ctx, ids)
}
func (s *Store) ListCommentsByUser(ctx context.Context, userID uint, page, pageSize int) ([]model.Comment, int64, error) {
	q := s.DB(ctx).Model(&model.Comment{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count user comments")
	}
	var comments []model.Comment
	err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&comments).Error
	return comments, total, errors.Wrap(err, "list user comments")
}
func (s *Store) DeleteCommentByUser(ctx context.Context, commentID, userID uint) error {
	defer s.InvalidateCache()
	result := s.DB(ctx).Model(&model.Comment{}).
		Where("id = ? AND user_id = ?", commentID, userID).
		Update("status", model.CommentDeleted)
	if result.RowsAffected == 0 {
		return errors.New("comment not found or not owned by user")
	}
	return errors.Wrap(result.Error, "delete user comment")
}
func (s *Store) ApprovedComments(ctx context.Context, postID uint) ([]model.Comment, error) {
	var comments []model.Comment
	err := s.DB(ctx).Where("post_id = ? AND status = ?", postID, model.CommentApproved).
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
	if err := visibleCommentsQuery(s.DB(ctx).Model(&model.Comment{}), postID, currentUserID, pendingCommentIDs).
		Count(&totalAll).Error; err != nil {
		return nil, errors.Wrap(err, "count visible comments")
	}

	qTop := visibleCommentsQuery(s.DB(ctx).Model(&model.Comment{}), postID, currentUserID, pendingCommentIDs).
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

	// 递归获取当前页顶层评论的所有子孙评论，支持多层嵌套的历史数据。
	// rootByID 记录每条评论对应的根顶层评论 ID。
	rootByID := make(map[uint]uint, len(tops))
	for _, top := range tops {
		rootByID[top.ID] = top.ID
	}

	var allDescendants []model.Comment
	frontier := make([]uint, len(tops))
	for i, top := range tops {
		frontier[i] = top.ID
	}
	for len(frontier) > 0 {
		var nextGen []model.Comment
		if err := visibleCommentsQuery(s.DB(ctx).Model(&model.Comment{}), postID, currentUserID, pendingCommentIDs).
			Where("parent_id IN ?", frontier).
			Order("created_at ASC").Find(&nextGen).Error; err != nil {
			return nil, errors.Wrap(err, "list child comments")
		}
		frontier = frontier[:0]
		for i := range nextGen {
			c := &nextGen[i]
			if _, seen := rootByID[c.ID]; seen {
				continue
			}
			rootByID[c.ID] = rootByID[c.ParentID]
			allDescendants = append(allDescendants, *c)
			frontier = append(frontier, c.ID)
		}
	}

	// 将深度 >1 的子孙评论的 ParentID 重写为根顶层评论 ID，实现平铺展示。
	// 同时，如果 ReplyToID 为 0（直接回复父评论），则设为原始 ParentID，
	// 以便 populateReplyToAuthors 能正确显示"回复 @某人"。
	for i := range allDescendants {
		origParent := allDescendants[i].ParentID
		root := rootByID[origParent]
		if root != 0 && origParent != root {
			allDescendants[i].ParentID = root
			if allDescendants[i].ReplyToID == 0 {
				allDescendants[i].ReplyToID = origParent
			}
		}
	}

	// 按 ParentID（即根顶层评论 ID）分组。
	index := make(map[uint][]model.Comment, len(tops))
	for _, c := range allDescendants {
		index[c.ParentID] = append(index[c.ParentID], c)
	}

	out := make([]model.Comment, 0, len(tops)+len(allDescendants))
	for _, top := range tops {
		out = append(out, top)
		out = append(out, index[top.ID]...)
	}
	populateReplyToAuthors(s.gormDB, out)
	populateCommenterRoles(s.gormDB, out, postID)
	return &CommentPageResult{Comments: out, TotalComments: totalAll, TotalTop: totalTop, Page: page, Pages: pages}, nil
}
func (s *Store) CommentPageForID(ctx context.Context, commentID uint, pageSize int) int {
	return s.VisibleCommentPageForViewerID(ctx, commentID, pageSize, 0, nil)
}

// CommentPageForComment 与 CommentPageForID 相同，但直接使用已加载的评论对象，避免重复查询。
func (s *Store) CommentPageForComment(ctx context.Context, c *model.Comment, pageSize int) int {
	if pageSize < 1 {
		pageSize = 20
	}
	if c.Status != model.CommentApproved {
		return 1
	}
	rootID := c.ID
	rootCreatedAt := c.CreatedAt
	if c.ParentID != 0 {
		var parent model.Comment
		if err := s.DB(ctx).Select("id", "created_at", "status", "user_id").
			First(&parent, c.ParentID).
			Error; err != nil {
			return 1
		}
		if parent.Status != model.CommentApproved {
			return 1
		}
		rootID = parent.ID
		rootCreatedAt = parent.CreatedAt
	}
	var newer int64
	_ = s.DB(ctx).Model(&model.Comment{}).
		Where("post_id = ? AND status = ?", c.PostID, model.CommentApproved).
		Where("parent_id = 0").
		Where("created_at > ? OR (created_at = ? AND id > ?)", rootCreatedAt, rootCreatedAt, rootID).
		Count(&newer).Error
	return int(newer)/pageSize + 1
}

// commentPageResult 用于批量计算评论页码的中间结果。
type commentPageResult struct {
	ID        uint
	CreatedAt time.Time
}

// CommentPagesForComments 批量计算多条评论的页码，避免 N+1 查询。
func (s *Store) CommentPagesForComments(ctx context.Context, comments []*model.Comment, pageSize int) map[uint]int {
	if pageSize < 1 {
		pageSize = 20
	}
	result := make(map[uint]int, len(comments))
	if len(comments) == 0 {
		return result
	}

	// 1. 收集需要查父评论的回复
	parentIDs := make([]uint, 0)
	parentMap := make(map[uint]*model.Comment) // childID -> parent
	for _, c := range comments {
		if c.Status != model.CommentApproved {
			result[c.ID] = 1
			continue
		}
		if c.ParentID != 0 {
			parentIDs = append(parentIDs, c.ParentID)
		}
	}
	if len(parentIDs) > 0 {
		var parents []model.Comment
		if err := s.DB(ctx).Select("id", "created_at", "status").
			Where("id IN ?", parentIDs).Find(&parents).Error; err == nil {
			for i := range parents {
				for _, c := range comments {
					if c.ParentID == parents[i].ID {
						parentMap[c.ID] = &parents[i]
					}
				}
			}
		}
	}

	// 2. 按 post 分组，批量查每个 post 的顶层评论列表
	postRoots := make(map[uint][]commentPageResult)
	postIDs := make(map[uint]bool)
	for _, c := range comments {
		if _, done := result[c.ID]; done {
			continue
		}
		postIDs[c.PostID] = true
	}
	for pid := range postIDs {
		var roots []commentPageResult
		s.DB(ctx).Model(&model.Comment{}).
			Select("id", "created_at").
			Where("post_id = ? AND status = ? AND parent_id = 0", pid, model.CommentApproved).
			Order("created_at ASC, id ASC").Find(&roots)
		postRoots[pid] = roots
	}

	// 3. 计算每条评论的页码
	for _, c := range comments {
		if _, done := result[c.ID]; done {
			continue
		}
		rootID := c.ID
		rootCreatedAt := c.CreatedAt
		if c.ParentID != 0 {
			if parent, ok := parentMap[c.ID]; ok && parent.Status == model.CommentApproved {
				rootID = parent.ID
				rootCreatedAt = parent.CreatedAt
			} else {
				result[c.ID] = 1
				continue
			}
		}
		roots := postRoots[c.PostID]
		newer := 0
		for _, r := range roots {
			if r.CreatedAt.After(rootCreatedAt) || (r.CreatedAt.Equal(rootCreatedAt) && r.ID > rootID) {
				newer++
			}
		}
		result[c.ID] = newer/pageSize + 1
	}
	return result
}
func (s *Store) VisibleCommentPageForID(ctx context.Context, commentID uint, pageSize int, currentUserID uint) int {
	return s.VisibleCommentPageForViewerID(ctx, commentID, pageSize, currentUserID, nil)
}
func (s *Store) VisibleCommentPageForViewerID(ctx context.Context, commentID uint, pageSize int, currentUserID uint, pendingCommentIDs []uint) int {
	if pageSize < 1 {
		pageSize = 20
	}
	var c model.Comment
	if err := s.DB(ctx).Select("id", "post_id", "parent_id", "created_at", "status", "user_id").
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
		if err := s.DB(ctx).Select("id", "created_at", "status", "user_id").
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
	_ = visibleCommentsQuery(s.DB(ctx).Model(&model.Comment{}), c.PostID, currentUserID, pendingCommentIDs).
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
	if err := s.DB(ctx).
		Select("id", "post_id", "parent_id", "status", "user_id").
		First(&target, replyToID).Error; err != nil {
		return 0, 0, errors.Wrap(err, "get reply target")
	}
	if target.PostID != postID || (target.Status != model.CommentApproved && !commentVisibleToViewer(target, currentUserID, pendingCommentIDs)) {
		return 0, 0, errors.New("reply target not visible")
	}
	if target.ParentID == 0 {
		return target.ID, target.ID, nil
	}
	var parent model.Comment
	if err := s.DB(ctx).Select("id", "post_id", "status", "user_id").First(&parent, target.ParentID).Error; err != nil {
		return 0, 0, errors.Wrap(err, "get reply parent")
	}
	if parent.PostID != postID || (parent.Status != model.CommentApproved && !commentVisibleToViewer(parent, currentUserID, pendingCommentIDs)) {
		return 0, 0, errors.New("reply parent not visible")
	}
	return parent.ID, target.ID, nil
}
func (s *Store) CreateComment(ctx context.Context, c *model.Comment) error {
	defer s.InvalidateCache()
	return errors.Wrap(s.DB(ctx).Create(c).Error, "保存评论失败")
}
func (s *Store) GetCommentByID(ctx context.Context, id uint) (*model.Comment, error) {
	var c model.Comment
	if err := s.DB(ctx).First(&c, id).Error; err != nil {
		return nil, errors.Wrap(err, "get comment by id")
	}
	return &c, nil
}
func (s *Store) CommentsByIDs(ctx context.Context, ids []uint) ([]model.Comment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var comments []model.Comment
	if err := s.DB(ctx).Where("id IN ?", ids).Find(&comments).Error; err != nil {
		return nil, errors.Wrap(err, "comments by ids")
	}
	return comments, nil
}
func (s *Store) RecentCommentCountByIP(ctx context.Context, ip string, sinceUnix int64) (int64, error) {
	var n int64
	err := s.DB(ctx).Model(&model.Comment{}).
		Where("ip = ? AND created_at > datetime(?, 'unixepoch')", ip, sinceUnix).
		Count(&n).Error
	return n, errors.Wrap(err, "recent comment count")
}
func (s *Store) RecentComments(ctx context.Context, n int) []model.Comment {
	var cs []model.Comment
	s.DB(ctx).Where("status = ?", model.CommentApproved).
		Order("created_at DESC").Limit(n).Find(&cs)
	return cs
}
func (s *Store) RecentCommentItems(ctx context.Context, n, pageSize int) []CommentWidgetItem {
	comments := s.RecentComments(ctx, n)
	return s.buildCommentWidgetItems(ctx, comments, pageSize)
}
func (s *Store) SayingComments(ctx context.Context, postID uint, n int) []model.Comment {
	var cs []model.Comment
	s.DB(ctx).Where("post_id = ? AND status = ? AND user_id = (?)",
		postID, model.CommentApproved,
		s.DB(ctx).Model(&model.Post{}).Select("author_id").Where("id = ?", postID)).
		Order("created_at DESC").Limit(n).Find(&cs)
	return cs
}
func (s *Store) SayingCommentItems(ctx context.Context, postID uint, n, pageSize int) []CommentWidgetItem {
	comments := s.SayingComments(ctx, postID, n)
	return s.buildCommentWidgetItems(ctx, comments, pageSize)
}
