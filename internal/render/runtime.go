package render

import (
	"html/template"
	"strings"
	"sync"

	"github.com/youthlin/wenlog/hook"
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
	WidgetsProvider func(ctx *RequestContext, area string) []WidgetInfo
	ThemeOption     func(ctx *RequestContext, optionID string) string
	Hooks           hook.Executor
	HookInvoke      func(ctx *RequestContext, name string, args ...any) any
	WidgetResolver  hook.WidgetResolver
}

func (tr *TemplateRuntime) current() TemplateProviders {
	if tr == nil {
		return TemplateProviders{}
	}
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.providers
}

// ConfigureTemplateRuntime 一次性配置主题模板运行时回调（启动时由 BindTemplateFunctions 调用）。
func (r *Renderer) ConfigureTemplateRuntime(providers TemplateProviders) {
	if r == nil {
		return
	}
	r.themeRuntime.mu.Lock()
	r.themeRuntime.providers = providers
	r.themeRuntime.mu.Unlock()
}

// SetHookInvokeProvider 更新 hookInvoke 模板函数（主题重载时调用）。
func (r *Renderer) SetHookInvokeProvider(fn func(ctx *RequestContext, name string, args ...any) any) {
	if r == nil {
		return
	}
	r.themeRuntime.mu.Lock()
	r.themeRuntime.providers.HookInvoke = fn
	r.themeRuntime.mu.Unlock()
}

// SetThemeWidgetsProvider 更新 renderWidgets 获取区域组件列表的实现。
func (r *Renderer) SetThemeWidgetsProvider(fn func(ctx *RequestContext, area string) []WidgetInfo) {
	if r == nil {
		return
	}
	r.themeRuntime.mu.Lock()
	r.themeRuntime.providers.WidgetsProvider = fn
	r.themeRuntime.mu.Unlock()
}

// SetWidgetResolver 设置统一的组件查找器（启动时由 populateWidgetRegistry 调用）。
func (r *Renderer) SetWidgetResolver(resolver hook.WidgetResolver) {
	if r == nil {
		return
	}
	r.themeRuntime.mu.Lock()
	r.themeRuntime.providers.WidgetResolver = resolver
	r.themeRuntime.mu.Unlock()
}

// doAction 触发一个模板 action hook，并收集主题/插件写入的 HTML。
// 相当于 WordPress 的 do_action()。
func doAction(ctx *RequestContext, name string, data any) template.HTML {
	h := hooks(ctx.Runtime)
	if h == nil || name == "" {
		return ""
	}
	var result strings.Builder
	h.DoAction(requestContext(ctx), name, &result, data)
	return template.HTML(result.String())
}

// applyFilter 应用过滤器链到输入值，返回过滤后的 HTML 结果。
// 模板层使用该函数时，filter 合约必须返回可信 HTML；纯文本输出应优先由宿主封装成专用模板函数。
// 相当于 WordPress 的 apply_filters()。
func applyFilter(ctx *RequestContext, value any, name string, args ...any) template.HTML {
	h := hooks(ctx.Runtime)
	if h == nil || name == "" {
		return trustedHTMLFromFilterValue(value)
	}
	return trustedHTMLFromFilterValue(h.ApplyFilters(requestContext(ctx), name, value, args...))
}

func hooks(runtime *TemplateRuntime) hook.Executor {
	return runtime.current().Hooks
}
