package theme

import (
	"context"
	"strings"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/store"
)

// Widget 是一个可渲染的内容块。
// 每个 Widget 封装自己的数据查询和 HTML 渲染逻辑。
type Widget interface {
	// Name 返回 widget 的唯一标识名，对应 theme.yaml 中 widgets 列表的值。
	Name() string
	// Data 查询并组装模板数据。返回 nil 表示该 widget 当前不需要渲染。
	Data(ctx context.Context, st *store.Store, settings WidgetSettings) (any, error)
}

// WidgetSettings 是 Widget 渲染时需要的站点设置与当前请求上下文。
type WidgetSettings struct {
	SayingPostID     uint
	DefaultAvatar    string
	CurrentUserID    uint
	CurrentUserName  string // DisplayName or Username
	RegistrationOpen bool
	Keyword          string // 搜索关键词
	CSRFToken        string

	// 预查询数据，避免 widget Data() 重复查询。
	// 由 base() 填充，widget 优先使用这些数据而非重新查询。
	RecentPosts        []model.Post
	RecentCommentItems []store.CommentWidgetItem
	SayingCommentItems []store.CommentWidgetItem
	ArchiveMonths      []store.ArchiveMonth
	Categories         []model.Category
	Tags               []model.Tag
	ThemeName          string // 缓存的当前主题名，避免重复查库
}

// UserInfoWidgetData 是 user_info widget 的模板数据。
type UserInfoWidgetData struct {
	CurrentUserID    uint
	CurrentUserName  string
	RegistrationOpen bool
	CSRFToken        string
}

// SearchWidgetData 是 search widget 的模板数据。
type SearchWidgetData struct {
	Keyword string
}

// SayingWidgetData 是 saying widget 的模板数据。
type SayingWidgetData struct {
	Items         []store.CommentWidgetItem
	AuthorName    string
	AuthorEmail   string
	DefaultAvatar string
}

// RecentPostsWidgetData 是 recent_posts widget 的模板数据。
type RecentPostsWidgetData struct {
	Posts []model.Post
}

// RecentCommentsWidgetData 是 recent_comments widget 的模板数据。
type RecentCommentsWidgetData struct {
	Items         []store.CommentWidgetItem
	DefaultAvatar string
}

// ArchiveMonthsWidgetData 是 archive_months widget 的模板数据。
type ArchiveMonthsWidgetData struct {
	Months []store.ArchiveMonth
}

// CategoriesWidgetData 是 categories widget 的模板数据。
type CategoriesWidgetData struct {
	Categories []model.Category
}

// TagsWidgetData 是 tags widget 的模板数据。
type TagsWidgetData struct {
	Tags []model.Tag
}

// registry 是全局 Widget 注册表。
var registry = map[string]Widget{}

// Register 注册一个 Widget。通常在 init() 中调用。
func Register(w Widget) {
	registry[w.Name()] = w
}

// Get 按名称获取已注册的 Widget，未注册返回 nil。
func Get(name string) Widget {
	return registry[name]
}

// RegisteredNames 返回所有已注册 Widget 的名称列表。
func RegisteredNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// WidgetTemplateName 返回主题可覆盖的 widget 模板名。
// 例如 recent_posts 对应 {{define "widget_recent_posts"}}。
func WidgetTemplateName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "-", "_")
	return "widget_" + name
}
