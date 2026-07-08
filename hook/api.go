// Package hook 提供宿主暴露给主题和插件脚本的统一扩展 API。
package hook

import (
	"context"
	"log/slog"

	"github.com/youthlin/wenlog/internal/store"
)

// API 是暴露给插件脚本的宿主能力。
type API struct {
	log          *slog.Logger      // 打日志
	funcs        map[string]Func   // 保存注册的函数
	domain       string            // 翻译文本域
	dataLoader   *store.DataLoader // 数据获取
	ctx          context.Context   // 请求上下文
	options      []OptionDecl      // 主题的选项声明
	addAction    func(name string, fn any, priority ...int)
	addFilter    func(name string, fn any, priority ...int)
	removeAction func(name string)
	removeFilter func(name string)
	regErrors    []error
}

// ========== ========== API 构造 ========== ==========

// NewAPI 创建一个 *API 实例
func NewAPI() *API {
	return &API{
		log:   slog.Default().With("component", "hook"),
		funcs: make(map[string]Func),
	}
}

func (api *API) WithDomain(domain string) *API {
	api.domain = domain
	return api
}

func (api *API) Domain() string { return api.domain }

// WithLoader 设置当前模板渲染请求的 DataLoader。
func (api *API) WithLoader(loader *store.DataLoader) *API {
	api.dataLoader = loader
	return api
}

// SetHookRegistrars 设置当前扩展 hook 注册函数，由宿主在加载插件或主题时注入。
func (api *API) SetHookRegistrars(addAction, addFilter func(name string, fn any, priority ...int)) *API {
	api.addAction = addAction
	api.addFilter = addFilter
	return api
}

// SetRemoveHooks 设置当前扩展 hook 移除函数，由宿主在加载插件或主题时注入。
func (api *API) SetRemoveHooks(removeAction, removeFilter func(name string)) *API {
	api.removeAction = removeAction
	api.removeFilter = removeFilter
	return api
}

// WithContext 返回绑定到当前请求 context 的 API 副本。
func (api *API) WithContext(ctx context.Context) *API {
	clone := *api
	clone.ctx = ctx
	if loader := getDataLoader(ctx); loader != nil {
		clone.dataLoader = loader
	}
	return &clone
}

func (api *API) loader() *store.DataLoader {
	if api == nil {
		return nil
	}
	if api.ctx != nil {
		if loader, _ := api.ctx.Value(loaderContextKey{}).(*store.DataLoader); loader != nil {
			return loader
		}
	}
	return api.dataLoader
}
