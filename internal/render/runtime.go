package render

import (
	"context"
	"html/template"
	"strings"
	"sync"
)

// TemplateRuntime 保存主题模板函数需要调用的运行时能力。
// 它归属于 Renderer 实例，避免不同渲染器之间通过包级全局变量串联。
type TemplateRuntime struct {
	mu        sync.RWMutex
	providers TemplateProviders
}

// TemplateProviders 集中保存主题模板函数需要回调宿主的能力。
//
// 之前这些能力分散在多个 atomic.Pointer + SetXxxProvider 方法中，启动主流程需要理解
// “哪个 Manager 给 Renderer 注入哪个函数”。集中成一个对象后，cmd/server/manager 只需要
// 读成“把模板运行时能力挂到 Renderer 上”，主题作者面对的模板函数也更容易对应到底层能力。
type TemplateProviders struct {
	HookInvoke   func(ctx *RequestContext, name string, args ...any) any
	ThemeOption  func(ctx *RequestContext, optionID string) string
	ThemeWidgets func(ctx *RequestContext, area string) []WidgetInfo
	Hooks        hookProvider
	PluginWidget func(ctx *RequestContext, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool)
}

type hookProvider interface {
	DoAction(ctx context.Context, name string, args ...any)
	ApplyFilters(ctx context.Context, name string, value any, args ...any) any
}

func (tr *TemplateRuntime) current() TemplateProviders {
	if tr == nil {
		return TemplateProviders{}
	}
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.providers
}

// ConfigureTemplateRuntime 一次性配置主题模板运行时回调。
func (r *Renderer) ConfigureTemplateRuntime(providers TemplateProviders) {
	if r == nil {
		return
	}
	r.themeRuntime.mu.Lock()
	r.themeRuntime.providers = providers
	r.themeRuntime.mu.Unlock()
}

func (r *Renderer) patchTemplateRuntime(patch func(*TemplateProviders)) {
	if r == nil || patch == nil {
		return
	}
	providers := r.themeRuntime.current()
	patch(&providers)
	r.ConfigureTemplateRuntime(providers)
}

// SetHookInvokeProvider 设置 hookInvoke 模板函数的实现（由 theme.Manager 注入）。
func (r *Renderer) SetHookInvokeProvider(fn func(ctx *RequestContext, name string, args ...any) any) {
	r.patchTemplateRuntime(func(providers *TemplateProviders) { providers.HookInvoke = fn })
}

// SetOptionProvider 设置 option 读取函数。
func (r *Renderer) SetOptionProvider(fn func(ctx *RequestContext, optionID string) string) {
	r.patchTemplateRuntime(func(providers *TemplateProviders) { providers.ThemeOption = fn })
}

// SetThemeWidgetsProvider 设置 renderWidgets 获取区域组件列表的实现。
func (r *Renderer) SetThemeWidgetsProvider(fn func(ctx *RequestContext, area string) []WidgetInfo) {
	r.patchTemplateRuntime(func(providers *TemplateProviders) { providers.ThemeWidgets = fn })
}

// SetHookProvider 设置 Hook Registry，用于 slot/postContent/commentContent 等模板函数。
func (r *Renderer) SetHookProvider(provider hookProvider) {
	r.patchTemplateRuntime(func(providers *TemplateProviders) { providers.Hooks = provider })
}

// SetPluginWidgetProvider 设置插件组件渲染函数。
func (r *Renderer) SetPluginWidgetProvider(fn func(ctx *RequestContext, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool)) {
	r.patchTemplateRuntime(func(providers *TemplateProviders) { providers.PluginWidget = fn })
}

// slot 触发一个模板 slot action，并收集主题/插件写入的 HTML。
func slot(runtime *TemplateRuntime, ctx *RequestContext, name string, data any) template.HTML {
	h := hooks(runtime)
	if h == nil || name == "" {
		return ""
	}
	var result strings.Builder
	h.DoAction(requestContext(ctx), name, &result, data)
	return template.HTML(result.String())
}

func hooks(runtime *TemplateRuntime) hookProvider {
	return runtime.current().Hooks
}
