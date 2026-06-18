package theme

import (
	"context"
	"html/template"

	"github.com/youthlin/blog/internal/store"
)

// Widget 是一个可渲染的侧栏内容块。
// 每个 Widget 封装自己的数据查询和 HTML 渲染逻辑。
type Widget interface {
	// Name 返回 widget 的唯一标识名，对应 theme.yaml 中 sidebar 列表的值。
	Name() string
	// Render 查询数据并渲染为 HTML 片段。
	// ctx 是请求上下文，st 用于数据库查询，settings 是当前站点设置。
	Render(ctx context.Context, st *store.Store, settings WidgetSettings) (template.HTML, error)
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
