// Package hook 提供宿主暴露给主题和插件脚本的统一扩展 API。
package hook

import (
	"context"
	"log/slog"

	"github.com/youthlin/wenlog/internal/store"
)

// API 是暴露给插件脚本的宿主能力。
type API struct {
	log        *slog.Logger      // 打日志
	funcs      map[string]Func   // 保存注册的函数
	domain     string            // 翻译文本域
	dataLoader *store.DataLoader // 数据获取
	ctx        context.Context   // 请求上下文
	options    []OptionDecl      // 主题的选项声明
	registry   RegistryBinding
	regErrors  []error
}

// ========== ========== API 构造 ========== ==========

// RegistryBinding 是 API 注册 action/filter 时使用的 Hook Registry 绑定。
type RegistryBinding struct {
	AddAction    func(name string, fn any, priority ...int)
	AddFilter    func(name string, fn any, priority ...int)
	RemoveAction func(name string)
	RemoveFilter func(name string)
}

// NewRegistryBinding 创建绑定到指定来源的 Hook Registry 配置。
func NewRegistryBinding(registry Registry, source Source) RegistryBinding {
	if registry == nil {
		return RegistryBinding{}
	}
	return RegistryBinding{
		AddAction:    func(name string, fn any, priority ...int) { registry.AddAction(name, fn, source, priority...) },
		AddFilter:    func(name string, fn any, priority ...int) { registry.AddFilter(name, fn, source, priority...) },
		RemoveAction: func(name string) { registry.RemoveAction(name, source) },
		RemoveFilter: func(name string) { registry.RemoveFilter(name, source) },
	}
}

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

// WithRegistryBinding 设置当前扩展注册和移除 hook 使用的 Registry 绑定。
func (api *API) WithRegistryBinding(binding RegistryBinding) *API {
	api.registry = binding
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

// ========== ========== 日志方法 ========== ==========

// ctx 返回 API 绑定的请求 context，无则使用 context.Background()。
func (api *API) reqCtx() context.Context {
	if api != nil && api.ctx != nil {
		return api.ctx
	}
	return context.Background()
}

// Debug 输出 Debug 级别日志，用法同 slog.DebugContext，参数为 key/value 交替对。
func (api *API) Debug(msg string, args ...any) {
	if api == nil || api.log == nil {
		return
	}
	api.log.DebugContext(api.reqCtx(), msg, args...)
}

// Info 输出 Info 级别日志。
func (api *API) Info(msg string, args ...any) {
	if api == nil || api.log == nil {
		return
	}
	api.log.InfoContext(api.reqCtx(), msg, args...)
}

// Warn 输出 Warn 级别日志。
func (api *API) Warn(msg string, args ...any) {
	if api == nil || api.log == nil {
		return
	}
	api.log.WarnContext(api.reqCtx(), msg, args...)
}

// Error 输出 Error 级别日志。
func (api *API) Error(msg string, args ...any) {
	if api == nil || api.log == nil {
		return
	}
	api.log.ErrorContext(api.reqCtx(), msg, args...)
}
