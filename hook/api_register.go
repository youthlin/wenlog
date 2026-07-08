package hook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
)

// AddAction 注册 action。
//
// 推荐脚本使用 ActionFunc / func(*API, ...any) 获得请求级 API。
// 也支持具体参数签名，例如 func(PostView) 或 func(context.Context, PostView)。
func (api *API) AddAction(name string, fn any, priority ...int) {
	if api == nil {
		return
	}
	if wrapped, ok := api.wrapAction(fn); ok {
		if err := api.validateRegistration("action", name, wrapped, true); err != nil {
			api.addRegistrationError(err)
			return
		}
		api.registry.AddAction(name, wrapped, priority...)
		return
	}
	if err := api.validateAction(name, fn); err != nil {
		api.addRegistrationError(err)
		return
	}
	api.registry.AddAction(name, fn, priority...)
}

func (api *API) wrapAction(fn any) (func(context.Context, ...any), bool) {
	switch f := fn.(type) {
	case ActionFunc:
		return func(ctx context.Context, args ...any) { f(api.WithContext(ctx), args...) }, true
	case func(*API):
		return func(ctx context.Context, _ ...any) { f(api.WithContext(ctx)) }, true
	}
	return nil, false
}

// AddFilter 注册 filter。
//
// 推荐脚本使用具体参数签名，例如 func(string, PostView) string；
// 需要请求级 context 时可把 context.Context 放在第一参。
// 需要完整 API 时使用 FilterFunc / func(*API, any, ...any) any。
func (api *API) AddFilter(name string, fn any, priority ...int) {
	if api == nil {
		return
	}
	if wrapped, ok := api.wrapFilter(fn); ok {
		if err := api.validateRegistration("filter", name, wrapped, true); err != nil {
			api.addRegistrationError(err)
			return
		}
		api.registry.AddFilter(name, wrapped, priority...)
		return
	}
	if err := api.validateFilter(name, fn); err != nil {
		api.addRegistrationError(err)
		return
	}
	api.registry.AddFilter(name, fn, priority...)
}

func (api *API) wrapFilter(fn any) (func(context.Context, any, ...any) any, bool) {
	switch f := fn.(type) {
	case FilterFunc:
		return func(ctx context.Context, value any, args ...any) any { return f(api.WithContext(ctx), value, args...) }, true
	case func(*API, any) any:
		return func(ctx context.Context, value any, _ ...any) any { return f(api.WithContext(ctx), value) }, true
	}
	return nil, false
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
// 推荐签名为 func(api *hook.API, args hook.Args) any。为了降低迁移和编写门槛，
// 也兼容以下简化签名：
//
//   - func(args hook.Args) any
//   - func(api *hook.API) any
//   - func() any
func (api *API) RegisterFunc(name string, fn any) {
	wrapped := wrapTemplateFunc(fn)
	if api == nil {
		return
	}
	if name == "" {
		api.addRegistrationError(errors.New("注册模板函数失败: name 不能为空"))
		return
	}
	if wrapped == nil {
		api.addRegistrationError(fmt.Errorf("注册模板函数[%s]失败: 签名不支持", name))
		return
	}
	api.funcs[name] = wrapped
}

func wrapTemplateFunc(fn any) Func {
	switch f := fn.(type) {
	case nil:
		return nil
	case Func: // = func(api *API, args Args) any
		return f
	case func(Args) any:
		return func(_ *API, args Args) any { return f(args) }
	case func(*API) any:
		return func(api *API, _ Args) any { return f(api) }
	case func() any:
		return func(_ *API, _ Args) any { return f() }
	}
	return nil
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
		slog.InfoContext(ctx, "InvokeFunc: function not found", "name", name, "available", api.FuncNames())
		return nil
	}
	defer func() { _ = recover() }()
	result := fn(api.WithContext(ctx), Args(args))
	slog.InfoContext(ctx, "InvokeFunc: done", "name", name, "result_nil", result == nil)
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

func (api *API) validateAction(name string, fn any) error {
	if err := api.validateRegistration("action", name, fn, false); err != nil {
		return err
	}
	t := reflect.TypeOf(fn)
	if t.NumOut() != 0 {
		return fmt.Errorf("注册 action[%s]失败: 处理函数不能有返回值", name)
	}
	return nil
}

func (api *API) validateFilter(name string, fn any) error {
	if err := api.validateRegistration("filter", name, fn, false); err != nil {
		return err
	}
	t := reflect.TypeOf(fn)
	if t.NumOut() != 1 {
		return fmt.Errorf("注册 filter[%s]失败: 处理函数必须返回一个值", name)
	}
	if t.NumIn() == 0 {
		return fmt.Errorf("注册 filter[%s]失败: 处理函数必须接收当前值", name)
	}
	return nil
}

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
	t := reflect.TypeOf(fn)
	if t == nil || t.Kind() != reflect.Func {
		return fmt.Errorf("注册%s[%s]失败: 处理函数必须是函数", kind, name)
	}
	return nil
}
