package hook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
)

// AddAction 注册 action。
//
// 处理函数第一个参数必须是 *hook.API，且必须使用 ActionFunc 签名 func(*API, ...any)
// 或无额外参数的 func(*API)。
func (api *API) AddAction(name string, fn any, priority ...int) {
	if api == nil {
		return
	}
	wrapped, err := api.wrapAction(name, fn)
	if err != nil {
		api.addRegistrationError(err)
		return
	}
	if err := api.validateRegistration("action", name, wrapped, true); err != nil {
		api.addRegistrationError(err)
		return
	}
	api.registry.AddAction(name, wrapped, priority...)
}

// wrapAction 包装 action 回调：直接类型断言，不匹配则报错。
func (api *API) wrapAction(name string, fn any) (func(context.Context, ...any), error) {
	if fn == nil {
		return nil, fmt.Errorf("注册 action[%s]失败: 处理函数不能为空", name)
	}
	// ActionFunc = func(*API, ...any)
	if f, ok := fn.(ActionFunc); ok {
		return func(ctx context.Context, args ...any) {
			defer func() {
				if rec := recover(); rec != nil {
					slog.WarnContext(ctx, "hook action handler panic", "hook", name, "panic", rec)
				}
			}()
			f(api.WithContext(ctx), args...)
		}, nil
	}
	if f, ok := fn.(func(*API)); ok {
		return func(ctx context.Context, _ ...any) {
			defer func() {
				if rec := recover(); rec != nil {
					slog.WarnContext(ctx, "hook action handler panic", "hook", name, "panic", rec)
				}
			}()
			f(api.WithContext(ctx))
		}, nil
	}
	return nil, fmt.Errorf("注册 action[%s]失败: 处理函数签名必须是 func(*hook.API, ...any) 或 func(*hook.API)", name)
}

// AddFilter 注册 filter。
//
// 处理函数第一个参数必须是 *hook.API，第二个参数接收当前值，返回同类型值。
// 核心 filter 使用文档中的固定签名，通用 filter 使用 FilterFunc = func(*API, any, ...any) any。
func (api *API) AddFilter(name string, fn any, priority ...int) {
	if api == nil {
		return
	}
	wrapped, err := api.wrapFilter(name, fn)
	if err != nil {
		api.addRegistrationError(err)
		return
	}
	if err := api.validateRegistration("filter", name, wrapped, true); err != nil {
		api.addRegistrationError(err)
		return
	}
	api.registry.AddFilter(name, wrapped, priority...)
}

// wrapFilter 包装 filter 回调：核心 hook 按名称匹配具体签名直接类型断言，
// 通用 filter 使用 FilterFunc，不匹配则报错。
func (api *API) wrapFilter(name string, fn any) (func(context.Context, any, ...any) any, error) {
	if fn == nil {
		return nil, fmt.Errorf("注册 filter[%s]失败: 处理函数不能为空", name)
	}
	// 核心 hook 按名称匹配具体签名
	switch name {
	case FilterPostTitle, FilterPostExcerptHTML, FilterPostContentHTML, FilterPostFooterHTML:
		if f, ok := fn.(func(*API, string, PostView) string); ok {
			return api.wrapFilterStringPostView(f, name), nil
		}
	case FilterCommentContentHTML:
		if f, ok := fn.(func(*API, string, CommentView) string); ok {
			return api.wrapFilterStringCommentView(f, name), nil
		}
	case FilterWidgetRenderHTML:
		if f, ok := fn.(func(*API, string, WidgetRenderView) string); ok {
			return api.wrapFilterStringWidgetRenderView(f, name), nil
		}
	case FilterHeadMeta:
		if f, ok := fn.(func(*API, string, HeadMetaView) string); ok {
			return api.wrapFilterStringHeadMetaView(f, name), nil
		}
	case FilterCommentBeforeCreate:
		if f, ok := fn.(func(*API, *CommentPreCreateView) *CommentPreCreateView); ok {
			return api.wrapFilterCommentPreCreate(f, name), nil
		}
	}
	// 通用 FilterFunc = func(*API, any, ...any) any
	if f, ok := fn.(FilterFunc); ok {
		return func(ctx context.Context, value any, args ...any) any {
			defer func() {
				if rec := recover(); rec != nil {
					slog.WarnContext(ctx, "hook filter handler panic", "hook", name, "panic", rec)
				}
			}()
			return f(api.WithContext(ctx), value, args...)
		}, nil
	}
	if f, ok := fn.(func(*API, any) any); ok {
		return func(ctx context.Context, value any, _ ...any) any {
			defer func() {
				if rec := recover(); rec != nil {
					slog.WarnContext(ctx, "hook filter handler panic", "hook", name, "panic", rec)
				}
			}()
			return f(api.WithContext(ctx), value)
		}, nil
	}
	return nil, fmt.Errorf("注册 filter[%s]失败: 处理函数签名不匹配，请使用对应 hook 的标准签名或 func(*hook.API, any, ...any) any", name)
}

func (api *API) wrapFilterStringPostView(f func(*API, string, PostView) string, name string) func(context.Context, any, ...any) any {
	return func(ctx context.Context, value any, args ...any) (result any) {
		result = value
		defer func() {
			if rec := recover(); rec != nil {
				slog.WarnContext(ctx, "hook filter handler panic", "hook", name, "panic", rec)
			}
		}()
		s, ok := value.(string)
		if !ok {
			slog.WarnContext(ctx, "hook filter value type mismatch, returning original",
				"hook", name, "expected", "string", "got", fmt.Sprintf("%T", value))
			return
		}
		var pv PostView
		if len(args) > 0 {
			pv, _ = args[0].(PostView)
		}
		return f(api.WithContext(ctx), s, pv)
	}
}

func (api *API) wrapFilterStringCommentView(f func(*API, string, CommentView) string, name string) func(context.Context, any, ...any) any {
	return func(ctx context.Context, value any, args ...any) (result any) {
		result = value
		defer func() {
			if rec := recover(); rec != nil {
				slog.WarnContext(ctx, "hook filter handler panic", "hook", name, "panic", rec)
			}
		}()
		s, ok := value.(string)
		if !ok {
			slog.WarnContext(ctx, "hook filter value type mismatch, returning original",
				"hook", name, "expected", "string", "got", fmt.Sprintf("%T", value))
			return
		}
		var cv CommentView
		if len(args) > 0 {
			cv, _ = args[0].(CommentView)
		}
		return f(api.WithContext(ctx), s, cv)
	}
}

func (api *API) wrapFilterStringWidgetRenderView(f func(*API, string, WidgetRenderView) string, name string) func(context.Context, any, ...any) any {
	return func(ctx context.Context, value any, args ...any) (result any) {
		result = value
		defer func() {
			if rec := recover(); rec != nil {
				slog.WarnContext(ctx, "hook filter handler panic", "hook", name, "panic", rec)
			}
		}()
		s, ok := value.(string)
		if !ok {
			slog.WarnContext(ctx, "hook filter value type mismatch, returning original",
				"hook", name, "expected", "string", "got", fmt.Sprintf("%T", value))
			return
		}
		var wv WidgetRenderView
		if len(args) > 0 {
			wv, _ = args[0].(WidgetRenderView)
		}
		return f(api.WithContext(ctx), s, wv)
	}
}

func (api *API) wrapFilterStringHeadMetaView(f func(*API, string, HeadMetaView) string, name string) func(context.Context, any, ...any) any {
	return func(ctx context.Context, value any, args ...any) (result any) {
		result = value
		defer func() {
			if rec := recover(); rec != nil {
				slog.WarnContext(ctx, "hook filter handler panic", "hook", name, "panic", rec)
			}
		}()
		s, ok := value.(string)
		if !ok {
			slog.WarnContext(ctx, "hook filter value type mismatch, returning original",
				"hook", name, "expected", "string", "got", fmt.Sprintf("%T", value))
			return
		}
		var hv HeadMetaView
		if len(args) > 0 {
			hv, _ = args[0].(HeadMetaView)
		}
		return f(api.WithContext(ctx), s, hv)
	}
}

func (api *API) wrapFilterCommentPreCreate(f func(*API, *CommentPreCreateView) *CommentPreCreateView, name string) func(context.Context, any, ...any) any {
	return func(ctx context.Context, value any, _ ...any) (result any) {
		result = value
		defer func() {
			if rec := recover(); rec != nil {
				slog.WarnContext(ctx, "hook filter handler panic", "hook", name, "panic", rec)
			}
		}()
		v, ok := value.(*CommentPreCreateView)
		if !ok || v == nil {
			return
		}
		return f(api.WithContext(ctx), v)
	}
}

// RemoveAction 移除当前扩展注册的所有同名 action。
func (api *API) RemoveAction(name string) {
	if api == nil || api.registry.RemoveAction == nil || name == "" {
		return
	}
	api.registry.RemoveAction(name)
}

// RemoveFilter 移除当前扩展注册的所有同名 filter。
func (api *API) RemoveFilter(name string) {
	if api == nil || api.registry.RemoveFilter == nil || name == "" {
		return
	}
	api.registry.RemoveFilter(name)
}

// RegisterFunc 注册一个可在模板中通过 hook_invoke 调用的数据函数。
//
// 签名必须是 func(api *hook.API, args hook.Args) any。
func (api *API) RegisterFunc(name string, fn any) {
	if api == nil {
		return
	}
	if name == "" {
		api.addRegistrationError(errors.New("注册模板函数失败: name 不能为空"))
		return
	}
	wrapped, err := wrapTemplateFunc(name, fn)
	if err != nil {
		api.addRegistrationError(err)
		return
	}
	api.funcs[name] = wrapped
}

func wrapTemplateFunc(name string, fn any) (Func, error) {
	if fn == nil {
		return nil, fmt.Errorf("注册模板函数[%s]失败: 函数不能为空", name)
	}
	// 标准签名: func(api *API, args Args) any
	if f, ok := fn.(Func); ok {
		return f, nil
	}
	return nil, fmt.Errorf("注册模板函数[%s]失败: 签名必须是 func(*hook.API, hook.Args) any", name)
}

// GetFunc 获取已注册的命名函数。
func (api *API) GetFunc(name string) Func {
	if api == nil || name == "" {
		return nil
	}
	return api.funcs[name]
}

// FuncNames 返回所有已注册命名函数名称。
func (api *API) FuncNames() []string {
	if api == nil {
		return nil
	}
	names := make([]string, 0, len(api.funcs))
	for name := range api.funcs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// InvokeFunc 调用已注册的数据函数。
func (api *API) InvokeFunc(ctx context.Context, name string, args map[string]any) any {
	if api == nil || name == "" {
		return nil
	}
	fn := api.funcs[name]
	if fn == nil {
		slog.DebugContext(ctx, "InvokeFunc: function not found", "name", name, "available", api.FuncNames())
		return nil
	}
	defer func() { _ = recover() }()
	result := fn(api.WithContext(ctx), Args(args))
	slog.DebugContext(ctx, "InvokeFunc: done", "name", name, "result_nil", result == nil)
	return result
}

// RegistrationError 返回脚本 Register 阶段收集到的注册错误。
func (api *API) RegistrationError() error {
	if api == nil || len(api.regErrors) == 0 {
		return nil
	}
	return errors.Join(api.regErrors...)
}

func (api *API) addRegistrationError(err error) {
	if api == nil || err == nil {
		return
	}
	api.regErrors = append(api.regErrors, err)
}

// validateRegistration 检查基础注册合法性。
func (api *API) validateRegistration(kind, name string, fn any, wrapped bool) error {
	if name == "" {
		return fmt.Errorf("注册%s失败: name 不能为空", kind)
	}
	if fn == nil {
		return fmt.Errorf("注册%s[%s]失败: 处理函数不能为空", kind, name)
	}
	if api == nil || (kind == "action" && api.registry.AddAction == nil) || (kind == "filter" && api.registry.AddFilter == nil) {
		return fmt.Errorf("注册%s[%s]失败: hook registry 未注入", kind, name)
	}
	if wrapped {
		return nil
	}
	return fmt.Errorf("注册%s[%s]失败: 处理函数签名不合法", kind, name)
}
