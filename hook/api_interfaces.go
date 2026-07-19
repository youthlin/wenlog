package hook

import "context"

// Registrar 是注册 action/filter 所需的最小能力集合。
type Registrar interface {
	AddAction(name string, fn any, priority ...int)
	AddFilter(name string, fn any, priority ...int)
	RemoveAction(name string)
	RemoveFilter(name string)
	RegistrationError() error
}

// FuncInvoker 是调用模板扩展函数所需的最小能力集合。
type FuncInvoker interface {
	InvokeFunc(ctx context.Context, name string, args map[string]any) any
}

// FuncRegistry 是注册和查询模板扩展函数所需的最小能力集合。
type FuncRegistry interface {
	FuncInvoker
	RegisterFunc(name string, fn any)
	GetFunc(name string) Func
	FuncNames() []string
}

// RenderAPI 是 action 输出和 HTML 转义所需的最小能力集合。
type RenderAPI interface {
	Print(args ...any)
	Printf(format string, args ...any)
	Println(args ...any)
	EscapeHTML(s string) string
}

// I18nAPI 是脚本翻译所需的最小能力集合。
type I18nAPI interface {
	T(msg string, args ...any) string
	N(singular, plural string, n int, args ...any) string
	X(ctx, msg string, args ...any) string
	XN(ctx, singular, plural string, n int, args ...any) string
}

// OptionAPI 是读取插件选项、主题选项和站点设置所需的最小能力集合。
type OptionAPI interface {
	PluginOption(optionID, def string) string
	Setting(key string) string
	Settings(keys ...string) map[string]string
	GetOption(themeName, optionID string) string
}

// QueryAPI 是读取站点公开内容和生成前台 URL 所需的最小能力集合。
type QueryAPI interface {
	Posts() []PostView
	Post(postID any) *PostView
	Pages() []PostView
	PageBySlug(slug string) *PostView
	RecentPosts(n int) []PostView
	PostsByCategory(categorySlug string) []PostView
	PostsByTag(tagSlug string) []PostView
	PostsByYear(year int) []PostView
	PostsByYearMonth(year, month int) []PostView
	Categories() []CategoryView
	Tags() []TagView
	CommentsByPost(postID any) []CommentView
	RecentComments(n int) []CommentView
	Users() []UserView
	User(userID any) *UserView
	ArchiveMonths() []ArchiveMonthView
	PostURL(post any) string
	PageURL(post any) string
	CommentURL(post any, comment any) string
	CategoryURL(slug string) string
	TagURL(slug string) string
	Snippet(content any, n int) string
	AvatarURL(email, defaultAvatar string, size int) string
}

var (
	_ Registrar    = (*API)(nil)
	_ FuncInvoker  = (*API)(nil)
	_ FuncRegistry = (*API)(nil)
	_ RenderAPI    = (*API)(nil)
	_ I18nAPI      = (*API)(nil)
	_ OptionAPI    = (*API)(nil)
	_ QueryAPI     = (*API)(nil)
)
