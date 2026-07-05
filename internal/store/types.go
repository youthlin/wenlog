// Package store — 共享类型定义。
package store

import (
	"github.com/youthlin/wenlog/internal/model"
	"time"
)

type CommentCounts struct {
	Pending  int64
	Approved int64
	Spam     int64
}
type DashboardStats struct {
	PostsPublished int64
	PostsDraft     int64
	PagesPublished int64
	PagesDraft     int64
	Comments       CommentCounts
	CategoryCount  int64
	TagCount       int64
	UploadCount    int64
}
type ReaderStats struct {
	CommentCount        int64
	PendingCommentCount int64
	CommentedPostCount  int64
	CommentedPageCount  int64
}
type ExportUser struct {
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	Website     string    `json:"website,omitempty"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}
type ExportComment struct {
	ID        uint      `json:"id"`
	PostID    uint      `json:"post_id"`
	Author    string    `json:"author"`
	Email     string    `json:"email"`
	URL       string    `json:"url,omitempty"`
	Content   string    `json:"content"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
type UserExportData struct {
	User     ExportUser      `json:"user"`
	Comments []ExportComment `json:"comments"`
}
type CommentPageResult struct {
	Comments      []model.Comment
	TotalComments int64 // 当前访问者可见评论总数(含回复)
	TotalTop      int64 // 当前访问者可见顶层评论总数
	Page          int
	Pages         int
}
type CommentWidgetItem struct {
	Comment    model.Comment
	Post       model.Post
	CommentURL string
	AuthorURL  string
	Snippet    string
}
type ListPostsResult struct {
	Posts []model.Post
	Total int64
	Page  int
	Pages int
}
type ArchiveMonth struct {
	Year  int
	Month int
	Count int64
}
