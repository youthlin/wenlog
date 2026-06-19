// Package store — 工具函数与私有方法。
package store

import (
	"context"
	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/util"
	"gorm.io/gorm"
	"sort"
	"strconv"
	"strings"
)

var ErrLastAdmin = errors.New("at least one admin user is required")
var ErrPendingRegistrationNotFound = errors.New("pending registration not found")
var ErrPendingEmailChangeNotFound = errors.New("pending email change not found")
var ErrCannotDeleteUncategorized = errors.New("cannot delete uncategorized category")

func termQueryLike(keyword string) string {
	return "%" + strings.ToLower(strings.TrimSpace(keyword)) + "%"
}
func adminPostOrder(postType string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if postType == model.PostTypePage {
			return db.Order("menu_order ASC").Order("id ASC")
		}
		return db.Order("published_at DESC").Order("id DESC")
	}
}
func slugifyTag(name string) string {
	return util.URLSlugify(name)
}
func ensureUncategorizedCategory(tx *gorm.DB) (*model.Category, error) {
	var cat model.Category
	err := tx.Where("slug = ?", "uncategorized").First(&cat).Error
	if err == nil {
		return &cat, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.Wrap(err, "find uncategorized category")
	}
	cat = model.Category{Name: "未分类", Slug: "uncategorized"}
	if err := tx.Create(&cat).Error; err != nil {
		return nil, errors.Wrap(err, "create uncategorized category")
	}
	return &cat, nil
}
func (s *Store) softDeleteComments(ctx context.Context, ids []uint) error {
	allIDs, err := s.commentTreeIDs(ctx, ids)
	if err != nil {
		return err
	}
	if len(allIDs) == 0 {
		return nil
	}
	return errors.Wrap(
		s.db(ctx).Model(&model.Comment{}).
			Where("id IN ?", allIDs).
			Update("status", model.CommentDeleted).Error,
		"soft delete comments",
	)
}
func (s *Store) commentTreeIDs(ctx context.Context, ids []uint) ([]uint, error) {
	seen := make(map[uint]struct{}, len(ids))
	queue := append([]uint(nil), ids...)
	for len(queue) > 0 {
		frontier := make([]uint, 0, len(queue))
		for _, id := range queue {
			if id == 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			frontier = append(frontier, id)
		}
		if len(frontier) == 0 {
			break
		}
		var children []uint
		if err := s.db(ctx).Model(&model.Comment{}).
			Where("parent_id IN ? OR reply_to_id IN ?", frontier, frontier).
			Pluck("id", &children).
			Error; err != nil {
			return nil, errors.Wrap(err, "list child comments")
		}
		queue = children
	}
	allIDs := make([]uint, 0, len(seen))
	for id := range seen {
		allIDs = append(allIDs, id)
	}
	sort.Slice(allIDs, func(i, j int) bool { return allIDs[i] < allIDs[j] })
	return allIDs, nil
}
func countAdmins(db *gorm.DB) (int64, error) {
	var count int64
	if err := db.Model(&model.User{}).Where("role = ?", model.RoleAdmin).Count(&count).Error; err != nil {
		return 0, errors.Wrap(err, "count admin users")
	}
	return count, nil
}
func (s *Store) fillCommentCounts(ctx context.Context, posts []model.Post) {
	if len(posts) == 0 {
		return
	}
	ids := make([]uint, len(posts))
	for i := range posts {
		ids[i] = posts[i].ID
	}
	counts := s.ApprovedCommentCounts(ctx, ids)
	for i := range posts {
		posts[i].CommentCount = counts[posts[i].ID]
	}
}
func populateReplyToAuthors(db *gorm.DB, comments []model.Comment) {
	ids := make([]uint, 0)
	seen := map[uint]bool{}
	for _, c := range comments {
		if c.ReplyToID == 0 || c.ReplyToID == c.ParentID || seen[c.ReplyToID] {
			continue
		}
		seen[c.ReplyToID] = true
		ids = append(ids, c.ReplyToID)
	}
	if len(ids) == 0 {
		return
	}
	var targets []model.Comment
	if err := db.Select("id", "author").Where("id IN ?", ids).Find(&targets).Error; err != nil {
		return
	}
	authors := make(map[uint]string, len(targets))
	for _, target := range targets {
		authors[target.ID] = target.Author
	}
	for i := range comments {
		comments[i].ReplyToAuthor = authors[comments[i].ReplyToID]
	}
}
func populateCommenterRoles(db *gorm.DB, comments []model.Comment, postID uint) {
	// 收集所有评论者的 UserID
	userIDs := make([]uint, 0)
	seen := map[uint]bool{}
	for _, c := range comments {
		if c.UserID != nil && !seen[*c.UserID] {
			seen[*c.UserID] = true
			userIDs = append(userIDs, *c.UserID)
		}
	}
	if len(userIDs) == 0 {
		return
	}

	// 查询用户角色
	var users []model.User
	if err := db.Select("id", "role").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return
	}
	userRoles := make(map[uint]string, len(users))
	for _, u := range users {
		userRoles[u.ID] = u.Role
	}

	// 查询文章作者
	var post model.Post
	if err := db.Select("author_id").Where("id = ?", postID).First(&post).Error; err != nil {
		return
	}

	for i := range comments {
		if comments[i].UserID == nil {
			continue // 游客,CommenterRole 为空
		}
		uid := *comments[i].UserID
		if uid == post.AuthorID {
			comments[i].CommenterRole = "author"
		} else if userRoles[uid] == model.RoleAdmin {
			comments[i].CommenterRole = "admin"
		} else {
			comments[i].CommenterRole = "user"
		}
	}
}
func visibleCommentsQuery(q *gorm.DB, postID uint, currentUserID uint, pendingCommentIDs []uint) *gorm.DB {
	if currentUserID == 0 && len(pendingCommentIDs) == 0 {
		return q.Where("post_id = ? AND status = ?", postID, model.CommentApproved)
	}
	where := "post_id = ? AND (status = ?"
	args := []any{postID, model.CommentApproved}
	if currentUserID != 0 {
		where += " OR (status = ? AND user_id = ?)"
		args = append(args, model.CommentPending, currentUserID)
	}
	if len(pendingCommentIDs) > 0 {
		where += " OR (status = ? AND id IN ?)"
		args = append(args, model.CommentPending, pendingCommentIDs)
	}
	where += ")"
	return q.Where(where, args...)
}
func commentVisibleToViewer(c model.Comment, currentUserID uint, pendingCommentIDs []uint) bool {
	if c.Status != model.CommentPending {
		return false
	}
	if currentUserID != 0 && c.UserID != nil && *c.UserID == currentUserID {
		return true
	}
	for _, id := range pendingCommentIDs {
		if id == c.ID {
			return true
		}
	}
	return false
}
func (s *Store) buildCommentWidgetItems(ctx context.Context, comments []model.Comment, pageSize int) []CommentWidgetItem {
	if len(comments) == 0 {
		return nil
	}
	// 批量查询所有关联文章，避免 N+1
	postIDs := make([]uint, 0, len(comments))
	for _, c := range comments {
		postIDs = append(postIDs, c.PostID)
	}
	postMap, err := s.PostMetas(ctx, postIDs)
	if err != nil {
		return nil
	}

	items := make([]CommentWidgetItem, 0, len(comments))
	for i := range comments {
		c := &comments[i]
		p, ok := postMap[c.PostID]
		if !ok || p.Status != model.StatusPublished {
			continue
		}
		base := "/"
		if p.PostType == model.PostTypePage {
			base = permalink.Page(p)
		} else {
			base = permalink.Post(p)
		}
		// 近期评论大概率在第一页，省略 cpage 计算（省掉每篇文章一次 SQL），
		// 直接用 #comment-{id} fragment 定位。
		items = append(items, CommentWidgetItem{
			Comment:    *c,
			Post:       *p,
			CommentURL: base + "#comment-" + strconv.Itoa(int(c.ID)),
			AuthorURL:  strings.TrimSpace(c.URL),
			Snippet:    commentSnippet(c.Content, consts.CommentSnippetMaxRune),
		})
	}
	return items
}
func commentSnippet(s string, maxRunes int) string {
	s = strings.TrimSpace(stripTags(s))
	if maxRunes < 1 {
		return s
	}
	var out []rune
	for _, r := range []rune(s) {
		if len(out) >= maxRunes {
			return string(out) + "…"
		}
		out = append(out, r)
	}
	return string(out)
}
func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
