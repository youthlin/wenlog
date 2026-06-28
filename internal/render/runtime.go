package render

import (
	"context"
	"html/template"
	"strings"
	"sync/atomic"
)

// TemplateRuntime 保存主题模板函数需要调用的运行时能力。
// 它归属于 Renderer 实例，避免不同渲染器之间通过包级全局变量串联。
type TemplateRuntime struct {
	themeWidgetsProvider atomic.Pointer[themeWidgetsProviderHolder]
	hookInvokeProvider   atomic.Pointer[hookInvokeProviderHolder]
	optionProvider       atomic.Pointer[optionProviderHolder]
	hookProvider         atomic.Pointer[hookProviderHolder]
	pluginWidgetProvider atomic.Pointer[pluginWidgetProviderHolder]
}

type hookProvider interface {
	DoAction(ctx context.Context, name string, args ...any)
	ApplyFilters(ctx context.Context, name string, value any, args ...any) any
}

type optionProviderHolder struct {
	fn func(ctx *RequestContext, optionID string) string
}

type hookInvokeProviderHolder struct {
	fn func(ctx *RequestContext, name string, args ...any) any
}

type themeWidgetsProviderHolder struct {
	fn func(ctx *RequestContext, area string) []WidgetInfo
}

type hookProviderHolder struct {
	provider hookProvider
}

type pluginWidgetProviderHolder struct {
	fn func(ctx *RequestContext, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool)
}

// SetHookInvokeProvider 设置 hookInvoke 模板函数的实现（由 theme.Manager 注入）。
func (r *Renderer) SetHookInvokeProvider(fn func(ctx *RequestContext, name string, args ...any) any) {
	if r == nil {
		return
	}
	r.themeRuntime.hookInvokeProvider.Store(&hookInvokeProviderHolder{fn: fn})
}

// SetOptionProvider 设置 option 读取函数。
func (r *Renderer) SetOptionProvider(fn func(ctx *RequestContext, optionID string) string) {
	if r == nil {
		return
	}
	r.themeRuntime.optionProvider.Store(&optionProviderHolder{fn: fn})
}

// SetThemeWidgetsProvider 设置 renderWidgets 获取区域组件列表的实现。
func (r *Renderer) SetThemeWidgetsProvider(fn func(ctx *RequestContext, area string) []WidgetInfo) {
	if r == nil {
		return
	}
	r.themeRuntime.themeWidgetsProvider.Store(&themeWidgetsProviderHolder{fn: fn})
}

// SetHookProvider 设置插件 Hook Registry，用于 pluginSlot/postContent/commentContent 等模板函数。
func (r *Renderer) SetHookProvider(provider hookProvider) {
	if r == nil {
		return
	}
	r.themeRuntime.hookProvider.Store(&hookProviderHolder{provider: provider})
}

// SetPluginWidgetProvider 设置插件组件渲染函数。
func (r *Renderer) SetPluginWidgetProvider(fn func(ctx *RequestContext, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool)) {
	if r == nil {
		return
	}
	r.themeRuntime.pluginWidgetProvider.Store(&pluginWidgetProviderHolder{fn: fn})
}

// pluginSlot 触发一个模板 slot action，并收集插件写入的 HTML。
func pluginSlot(runtime *TemplateRuntime, ctx *RequestContext, name string, data any) template.HTML {
	h := hooks(runtime)
	if h == nil || name == "" {
		return ""
	}
	var result strings.Builder
	h.DoAction(requestContext(ctx), name, &result, data)
	return template.HTML(result.String())
}

func hooks(runtime *TemplateRuntime) hookProvider {
	if runtime == nil {
		return nil
	}
	provider := runtime.hookProvider.Load()
	if provider == nil {
		return nil
	}
	return provider.provider
}
