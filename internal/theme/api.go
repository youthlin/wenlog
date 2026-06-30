package theme

import (
	"github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/store"
)

type ThemeFunc = hook.Func
type PostView = hook.PostView
type CategoryView = hook.CategoryView
type TagView = hook.TagView
type CommentView = hook.CommentView
type UserView = hook.UserView
type ArchiveMonthView = hook.ArchiveMonthView

// API 是 hook.API 在 internal/theme 中的包装，负责桥接内部主题声明类型。
type API struct {
	*hook.API
	themeOptions []OptionDecl
}

// NewAPI 创建主题 Hook API 实例。
func NewAPI(loader *store.DataLoader) *API {
	return &API{API: hook.NewWithLoader(loader, "theme")}
}

// SetHookRegistry 设置当前主题注册 action/filter 使用的 Hook Registry。
func (api *API) SetHookRegistry(hooks hook.Registry, source hook.Source) {
	if api == nil || api.API == nil || hooks == nil {
		return
	}
	api.API.SetHookRegistrars(
		func(name string, fn any, priority ...int) { hooks.AddAction(name, fn, source, priority...) },
		func(name string, fn any, priority ...int) { hooks.AddFilter(name, fn, source, priority...) },
	)
	api.API.SetRemoveHooks(
		func(name string) { hooks.RemoveAction(name, source) },
		func(name string) { hooks.RemoveFilter(name, source) },
	)
}

// SetThemeOptions 设置主题声明的选项列表（含默认值），供 GetOption 回退使用。
func (api *API) SetThemeOptions(opts []OptionDecl) {
	if api == nil || api.API == nil {
		return
	}
	api.themeOptions = append(api.themeOptions[:0], opts...)
	api.API.SetThemeOptions(opts)
}
