package render

import (
	"context"
	"html/template"
	"strings"
	"sync"

	"github.com/youthlin/blog/hook"
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
	HookInvoke     func(ctx *RequestContext, name string, args ...any) any
	ThemeOption    func(ctx *RequestContext, optionID string) string
	ThemeWidgets   func(ctx *RequestContext, area string) []WidgetInfo
	Hooks          hookProvider
	PluginWidget   PluginWidgetRenderer // Deprecated: 使用 WidgetResolver
	WidgetResolver WidgetResolver
}

// WidgetResolver 根据来源和 ID 查找组件实现。
// source 为 "builtin" / "theme" / "plugin"，pluginID 仅在 source="plugin" 时有值。
type WidgetResolver func(source, id, pluginID string) hook.Widget

type hookProvider interface {
	DoAction(ctx context.Context, name string, args ...any)
	ApplyFilters(ctx context.Context, name string, value any, args ...any) any
}

// PluginWidgetRenderer 是插件组件渲染能力的接口（已废弃，保留用于兼容）。
// 新代码应使用 WidgetResolver。
type PluginWidgetRenderer interface {
	RenderWidget(ctx context.Context, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool)
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

// SetPluginWidgetProvider 设置插件组件渲染器（兼容旧接口，同时设置 WidgetResolver）。
func (r *Renderer) SetPluginWidgetProvider(renderer PluginWidgetRenderer) {
	r.patchTemplateRuntime(func(providers *TemplateProviders) {
		providers.PluginWidget = renderer
		if renderer != nil {
			providers.WidgetResolver = func(source, id, pluginID string) hook.Widget {
				if source != "plugin" {
					return nil
				}
				return &pluginWidgetAdapter{renderer: renderer, pluginID: pluginID, widgetID: id}
			}
		}
	})
}

// SetWidgetResolver 设置统一的组件查找器。
func (r *Renderer) SetWidgetResolver(resolver WidgetResolver) {
	r.patchTemplateRuntime(func(providers *TemplateProviders) { providers.WidgetResolver = resolver })
}

// pluginWidgetAdapter 将旧的 PluginWidgetRenderer 适配为 hook.Widget 接口。
type pluginWidgetAdapter struct {
	renderer PluginWidgetRenderer
	pluginID string
	widgetID string
}

func (a *pluginWidgetAdapter) Meta() hook.WidgetDecl {
	return hook.WidgetDecl{ID: a.widgetID, Source: "plugin", PluginID: a.pluginID}
}

func (a *pluginWidgetAdapter) Render(ctx context.Context, tpl *template.Template, instance hook.WidgetInstance, data any) (template.HTML, error) {
	html, ok := a.renderer.RenderWidget(ctx, a.pluginID, a.widgetID, instance.Settings, data)
	if !ok {
		return "", nil
	}
	return html, nil
}

// slot 触发一个模板 slot action，并收集主题/插件写入的 HTML。
func slot(runtime *TemplateRuntime, ctx *RequestContext, name string, data any) template.HTML {
	h := hooks(runtime)
	if h == nil || name == "" {
		return ""
	}
	var result strings.Builder
	h.DoAction(requestContext(ctx), name, &result, hookDataForSlot(name, data))
	return template.HTML(result.String())
}

// hookDataForSlot 根据 hook 名称构造对应的结构化参数。
func hookDataForSlot(name string, data any) any {
	switch name {
	case hook.HookHeadEnd:
		return hook.HeadEndData{Data: data}
	case hook.HookBodyEnd:
		return hook.BodyEndData{Data: data}
	case hook.HookCommentFormAfterTextarea:
		return hook.CommentFormAfterTextareaData{Data: data}
	default:
		return data
	}
}

func hooks(runtime *TemplateRuntime) hookProvider {
	return runtime.current().Hooks
}
