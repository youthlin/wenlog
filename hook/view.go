package hook

import "time"

// PostView 是扩展 API 暴露的文章/页面只读视图。
type PostView struct {
	ID           uint
	Title        string
	Slug         string
	Excerpt      string
	Content      string
	AuthorID     uint
	Status       string
	PostType     string
	Views        int64
	MenuOrder    int
	PublishedAt  time.Time
	ModifiedAt   time.Time
	CommentCount int64
	Author       UserView
	Categories   []CategoryView
	Tags         []TagView
}

// CategoryView 是扩展 API 暴露的分类只读视图。
type CategoryView struct {
	ID          uint
	Name        string
	Slug        string
	Description string
	ParentID    uint
	PostCount   int64
}

// TagView 是扩展 API 暴露的标签只读视图。
type TagView struct {
	ID        uint
	Name      string
	Slug      string
	PostCount int64
}

// CommentView 是扩展 API 暴露的评论只读视图。
type CommentView struct {
	ID        uint
	PostID    uint
	ParentID  uint
	Author    string
	Email     string
	URL       string
	Content   string
	Status    string
	CreatedAt time.Time
}

// UserView 是扩展 API 暴露的用户只读视图。
type UserView struct {
	ID          uint
	Username    string
	DisplayName string
	Email       string
	Website     string
	Role        string
}

// ArchiveMonthView 是归档月份的只读视图。
type ArchiveMonthView struct {
	Year  int
	Month int
	Count int64
}
