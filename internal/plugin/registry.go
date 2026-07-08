// Package plugin 提供插件 manifest、Hook Registry 与插件运行时管理能力。
package plugin

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"sort"
	"sync"

	"github.com/youthlin/wenlog/hook"
)

const (
	// SourceCore 表示由宿主核心注册的 hook。
	SourceCore = "core"
	// SourceTheme 表示由当前主题注册的 hook。
	SourceTheme = "theme"
	// SourcePlugin 表示由插件注册的 hook。
	SourcePlugin = "plugin"

	// PriorityEarly 适合需要尽早执行的处理器。
	PriorityEarly = 5
	// PriorityDefault 是默认优先级。
	PriorityDefault = 10
	// PriorityLate 适合展示层收尾适配。
	PriorityLate = 20
)

// Source 是 hook.Source 的别名，记录一个 hook 处理器来自核心、主题还是插件。
type Source = hook.Source

// Handler 是注册到某个 action/filter 上的处理器。
type Handler struct {
	Name     string // 名称
	Priority int    // 优先级
	Source   Source // 来源
	Fn       any    // 函数实现
	order    int64  // 相同优先级时 按添加顺序
}

var _ sort.Interface = (Handlers)(nil)

type Handlers []Handler

// Len implements [sort.Interface].
func (h Handlers) Len() int {
	return len(h)
}

// Less implements [sort.Interface].
func (h Handlers) Less(i int, j int) bool {
	if h[i].Priority != h[j].Priority {
		return h[i].Priority < h[j].Priority
	}
	return h[i].order < h[j].order
}

// Swap implements [sort.Interface].
func (h Handlers) Swap(i int, j int) {
	h[i], h[j] = h[j], h[i]
}

// ActionFunc 是 action hook 的推荐函数签名。
// action 不返回值，只用于执行副作用或向输出 writer 写入内容。
type ActionFunc = func(ctx context.Context, args ...any)

// FilterFunc 是 filter hook 的推荐函数签名。
// filter 接收当前值并返回修改后的新值。
type FilterFunc = func(ctx context.Context, value any, args ...any) any

var _ hook.Registry = (*Registry)(nil)

// Registry 保存所有 action/filter 处理器。
type Registry struct {
	mu         sync.RWMutex
	actions    map[string]Handlers
	filters    map[string]Handlers
	didActions map[string]int
	next       int64
	log        *slog.Logger
}

// NewRegistry 创建一个空 Hook Registry。
func NewRegistry() *Registry {
	return &Registry{
		actions:    make(map[string]Handlers),
		filters:    make(map[string]Handlers),
		didActions: make(map[string]int),
		log:        slog.Default().With("component", "plugin-hooks"),
	}
}

// AddAction 注册 action 处理器。
func (r *Registry) AddAction(name string, fn any, source Source, priority ...int) {
	if r == nil || name == "" || fn == nil {
		return
	}
	r.add(r.actions, name, fn, source, firstPriority(priority))
}

// AddFilter 注册 filter 处理器。
func (r *Registry) AddFilter(name string, fn any, source Source, priority ...int) {
	if r == nil || name == "" || fn == nil {
		return
	}
	r.add(r.filters, name, fn, source, firstPriority(priority))
}

func firstPriority(values []int) int {
	if len(values) == 0 {
		return PriorityDefault
	}
	return values[0]
}

func (r *Registry) add(target map[string]Handlers, name string, fn any, source Source, priority int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	h := Handler{Name: name, Priority: priority, Source: source, Fn: fn, order: r.next}
	handlers := append(target[name], h)
	sort.Stable(handlers)
	target[name] = handlers
}

// RemoveAction 移除指定 source 注册的所有同名 action 处理器。
// 返回实际移除的数量。
func (r *Registry) RemoveAction(name string, source Source) int {
	if r == nil || name == "" {
		return 0
	}
	return r.remove(r.actions, name, source)
}

// RemoveFilter 移除指定 source 注册的所有同名 filter 处理器。
// 返回实际移除的数量。
func (r *Registry) RemoveFilter(name string, source Source) int {
	if r == nil || name == "" {
		return 0
	}
	return r.remove(r.filters, name, source)
}

func (r *Registry) remove(target map[string]Handlers, name string, source Source) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	handlers := target[name]
	if len(handlers) == 0 {
		return 0
	}
	kept := handlers[:0]
	removed := 0
	for _, h := range handlers {
		if h.Source.Type == source.Type && h.Source.ID == source.ID {
			removed++
		} else {
			kept = append(kept, h)
		}
	}
	if removed == 0 {
		return 0
	}
	if len(kept) == 0 {
		delete(target, name)
	} else {
		target[name] = kept
	}
	return removed
}

// Actions 返回指定 action 当前注册的处理器快照。
func (r *Registry) Actions(name string) []Handler {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneHandlers(r.actions[name])
}

// Filters 返回指定 filter 当前注册的处理器快照。
func (r *Registry) Filters(name string) []Handler {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneHandlers(r.filters[name])
}

func cloneHandlers(in []Handler) []Handler {
	if len(in) == 0 {
		return nil
	}
	out := make([]Handler, len(in))
	copy(out, in)
	return out
}

// DoAction 执行一个 action。单个处理器 panic 不会中断后续处理器。
// w 是输出 writer，会自动注入 context 供 api.Printf 使用。
func (r *Registry) DoAction(ctx context.Context, name string, w io.Writer, args ...any) {
	if r == nil {
		return
	}
	// 注入 writer 到 context
	ctx = hook.WithActionWriter(ctx, w)
	// 注入当前 hook 名到 context
	ctx = hook.WithCurrentHook(ctx, name)
	// 递增 didAction 计数
	r.mu.Lock()
	r.didActions[name]++
	r.mu.Unlock()
	for _, h := range r.Actions(name) {
		r.safeDoAction(ctx, h, args...)
	}
}

func (r *Registry) safeDoAction(ctx context.Context, h Handler, args ...any) {
	defer r.recoverHandler(ctx, h)
	switch fn := h.Fn.(type) {
	case ActionFunc:
		fn(ctx, args...)
	case func(...any):
		fn(args...)
	case func():
		fn()
	default:
		// 这里保留反射是为了兼容 yaegi 脚本里声明的具体函数签名。
		// 解释器导出的函数类型可能不是宿主侧的 type alias，但参数可赋值/可转换；
		// 不通过反射就只能要求插件作者全部写成 func(context.Context, ...any)，开发体验会明显变差。
		callByReflection(h.Fn, ctx, nil, args...)
	}
}

// ApplyFilters 依次执行 filter，并把上一个 filter 的返回值传给下一个 filter。
func (r *Registry) ApplyFilters(ctx context.Context, name string, value any, args ...any) any {
	current := value
	for _, h := range r.Filters(name) {
		current = r.safeApplyFilter(ctx, h, current, args...)
	}
	return current
}

// DidAction 返回指定 action 已被触发的次数。
func (r *Registry) DidAction(name string) int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.didActions[name]
}

// DoingAction 返回指定 action 当前是否正在执行中。
func (r *Registry) DoingAction(ctx context.Context, name string) bool {
	return hook.CurrentHook(ctx) == name
}

// ReplaceAll 原子替换所有 hook 处理器，用于插件重载场景。
// 编译到临时 Registry 成功后调用此方法，避免替换 Registry 实例本身。
func (r *Registry) ReplaceAll(actions, filters map[string]Handlers, didActions map[string]int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions = actions
	r.filters = filters
	r.didActions = didActions
}

func (r *Registry) safeApplyFilter(ctx context.Context, h Handler, value any, args ...any) (out any) {
	out = value
	defer func() {
		if rec := recover(); rec != nil {
			r.logPanic(ctx, h, rec)
			out = value
		}
	}()
	switch fn := h.Fn.(type) {
	case FilterFunc:
		return fn(ctx, value, args...)
	case func(any, ...any) any:
		return fn(value, args...)
	case func(any) any:
		return fn(value)
	default:
		// 同 safeDoAction：filter 也允许插件/主题使用具体签名（例如 func(context.Context, string) string），
		// 需要运行时按 hook 参数列表做一次安全适配。
		result, ok := callByReflection(h.Fn, ctx, value, args...)
		if !ok {
			return value
		}
		return result
	}
}

func (r *Registry) recoverHandler(ctx context.Context, h Handler) {
	if rec := recover(); rec != nil {
		r.logPanic(ctx, h, rec)
	}
}

func (r *Registry) logPanic(ctx context.Context, h Handler, rec any) {
	if r == nil || r.log == nil {
		return
	}
	r.log.WarnContext(ctx, "hook处理器执行失败",
		slog.String("hook", h.Name),
		slog.Int("priority", h.Priority),
		slog.String("source_type", h.Source.Type),
		slog.String("source_id", h.Source.ID),
		slog.Any("panic", rec),
	)
}

func callByReflection(fn any, ctx context.Context, value any, args ...any) (any, bool) {
	rv := reflect.ValueOf(fn)
	if !rv.IsValid() || rv.Kind() != reflect.Func {
		return value, false
	}
	rt := rv.Type()
	if !rt.IsVariadic() && rt.NumIn() < fixedArgCount(ctx, value) {
		return value, false
	}
	in := buildReflectArgs(rt, ctx, value, args...)
	if in == nil {
		return value, false
	}
	outs := rv.Call(in)
	if len(outs) == 0 {
		return value, true
	}
	if !outs[0].IsValid() || !outs[0].CanInterface() {
		return value, true
	}
	return outs[0].Interface(), true
}

func fixedArgCount(ctx context.Context, value any) int {
	n := 0
	if ctx != nil {
		n++
	}
	if value != nil {
		n++
	}
	return n
}

func buildReflectArgs(rt reflect.Type, ctx context.Context, value any, args ...any) []reflect.Value {
	values := make([]any, 0, len(args)+2)
	if ctx != nil {
		values = append(values, ctx)
	}
	if value != nil {
		values = append(values, value)
	}
	values = append(values, args...)

	if !rt.IsVariadic() && len(values) != rt.NumIn() {
		return nil
	}
	if rt.IsVariadic() && len(values) < rt.NumIn()-1 {
		return nil
	}
	out := make([]reflect.Value, 0, len(values))
	for i, v := range values {
		argType := rt.In(i)
		if rt.IsVariadic() && i >= rt.NumIn()-1 {
			argType = rt.In(rt.NumIn() - 1).Elem()
		}
		arg, ok := reflectArg(v, argType)
		if !ok {
			return nil
		}
		out = append(out, arg)
	}
	return out
}

func reflectArg(v any, typ reflect.Type) (reflect.Value, bool) {
	if v == nil {
		return reflect.Zero(typ), true
	}
	rv := reflect.ValueOf(v)
	if rv.Type().AssignableTo(typ) {
		return rv, true
	}
	if rv.Type().ConvertibleTo(typ) {
		return rv.Convert(typ), true
	}
	return reflect.Value{}, false
}
