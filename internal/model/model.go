// Package model 定义博客的 gorm 数据模型。
//
// 设计要点:Post 沿用 WordPress 原始 post_id 作为主键,以保证历史永久链接
// (/{年}{id}.html)完全不变。
package model

import "time"

// 文章/评论状态常量。
const (
	StatusPublished = "published"
	StatusDraft     = "draft"

	CommentApproved = "approved"
	CommentPending  = "pending"
	CommentSpam     = "spam"
	CommentDeleted  = "deleted"

	PostTypePost = "post"
	PostTypePage = "page"

	FormatHTML     = "html"
	FormatMarkdown = "markdown"

	// 用户角色常量。
	RoleAdmin      = "admin"
	RoleAuthor     = "author"
	RoleSubscriber = "subscriber"
)

// User 是后台登录用户。
type User struct {
	ID               uint   `gorm:"primaryKey"`
	Username         string `gorm:"uniqueIndex;size:64"`
	PasswordHash     string `gorm:"size:255"`
	DisplayName      string `gorm:"size:128"`
	Email            string `gorm:"size:128;index"`
	Role             string `gorm:"size:16;default:subscriber"`
	ResetToken       string `gorm:"size:128;index"`
	ResetTokenExpiry time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Post 既表示文章(post)也表示页面(page),由 PostType 区分。
type Post struct {
	// ID 沿用 WordPress 原始 post_id,是永久链接的核心。
	ID    uint   `gorm:"primaryKey"`
	Title string `gorm:"size:512"`
	// Slug 对应 WP post_name:页面用它做永久链接(/{slug}),文章一般为空。
	Slug string `gorm:"index;size:512"`
	// Content 是渲染后的正文 HTML(前台展示),保留 <!--more--> 分割标记。
	Content string `gorm:"type:text"`
	// ContentMD 是 Markdown 原文(后台编辑回填用;历史导入文章存清洗后的 HTML 当原文)。
	ContentMD string `gorm:"type:text"`
	// Excerpt 是摘要(无 more 标记时列表页兜底使用)。
	Excerpt string `gorm:"type:text"`
	// AuthorID 是作者用户 ID(关联 User)。
	AuthorID uint `gorm:"index"`
	// Status: published / draft。
	Status string `gorm:"index;size:16"`
	// PostType: post / page。
	PostType string `gorm:"index;size:16"`
	// ContentFormat: html(历史导入)/ markdown(后台新建)。
	ContentFormat string `gorm:"size:16"`
	// CommentStatus: open / closed。
	CommentStatus string `gorm:"size:16"`
	// Views 浏览量。
	Views int64
	// MenuOrder 页面在导航中的排序(仅 page 用)。
	MenuOrder   int
	PublishedAt time.Time `gorm:"index"`
	ModifiedAt  time.Time

	// CommentCount 仅用于渲染期展示,不入库。
	// 前台通常填充已批准评论数,后台列表可按场景填充总评论数。
	CommentCount int64 `gorm:"->;-:migration"`

	Author     User       `gorm:"foreignKey:AuthorID"`
	Categories []Category `gorm:"many2many:post_categories;"`
	Tags       []Tag      `gorm:"many2many:post_tags;"`
	Comments   []Comment  `gorm:"foreignKey:PostID"`
}

// Category 是文章分类,支持父子层级(ParentID)。
type Category struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"size:128"`
	Slug        string `gorm:"uniqueIndex;size:128"`
	Description string `gorm:"type:text"`
	ParentID    uint   `gorm:"index"`
	PostCount   int64  `gorm:"->;-:migration"`
}

// Tag 是文章标签。
type Tag struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:128"`
	Slug      string `gorm:"uniqueIndex;size:191"`
	PostCount int64  `gorm:"->;-:migration"`
}

// Comment 是评论,支持楼中楼(ParentID 指向父评论)。
type Comment struct {
	ID       uint `gorm:"primaryKey"`
	PostID   uint `gorm:"index"`
	ParentID uint `gorm:"index"`
	// ReplyToID 记录当前回复实际指向的评论;ParentID 仍用于两层树形归组。
	ReplyToID uint   `gorm:"index"`
	UserID    *uint  `gorm:"index"` // 登录用户评论时关联 User,匿名评论为 nil
	Author    string `gorm:"size:128"`
	Email     string `gorm:"size:128"`
	URL       string `gorm:"size:255"`
	IP        string `gorm:"size:64"`
	Content   string `gorm:"type:text"`
	// Status: approved / pending / spam / deleted。
	Status        string `gorm:"index;size:16"`
	NotifyOnReply bool
	CreatedAt     time.Time `gorm:"index"`

	// ReplyToAuthor 仅用于渲染“回复 @某人”,不入库。
	ReplyToAuthor string `gorm:"-"`
}

// Setting 是站点级键值设置,用于后台持久化站点名称/描述等。
type Setting struct {
	Key       string `gorm:"primaryKey;size:64"`
	Value     string `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Upload 是后台上传文件的元数据记录。
type Upload struct {
	ID         uint   `gorm:"primaryKey"`
	Path       string `gorm:"size:512;index"` // 站内 URL,如 /wp-content/uploads/2026/06/xxx.png
	OrigName   string `gorm:"size:255"`
	MimeType   string `gorm:"size:128"`
	Size       int64
	Width      int
	Height     int
	UploaderID uint      `gorm:"index"`
	CreatedAt  time.Time `gorm:"index"`
}
