package theme

import (
	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/store"
)

// API 是 hook.API 在 internal/theme 中的包装，负责桥接内部主题声明类型。
type API struct {
	*hook.API
	themeOptions []OptionDecl
}

// NewAPI 创建主题 Hook API 实例。
func NewAPI(domain string, loader *store.DataLoader, opts []OptionDecl) *API {
	hookAPI := hook.NewAPI().WithDomain(domain).WithLoader(loader).WithThemeOptions(opts)
	return &API{API: hookAPI, themeOptions: opts}
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
