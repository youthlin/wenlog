package hook

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"sort"
	"sync"
)

// Executor 是 Hook Registry 的最小接口，render 包只需要 DoAction 和 ApplyFilters。
type Executor interface {
	DoAction(ctx context.Context, name string, w io.Writer, args ...any)
	ApplyFilters(ctx context.Context, name string, value any, args ...any) any
}

// Registry 是 hook 注册与执行接口。
// theme 包通过此接口使用 hook 能力，plugin 包的 Registry 实现此接口。
type Registry interface {
	Executor
	AddAction(name string, fn any, source Source, priority ...int)
	AddFilter(name string, fn any, source Source, priority ...int)
	RemoveAction(name string, source Source) int
	RemoveFilter(name string, source Source) int
	DidAction(name string) int
	DoingAction(ctx context.Context, name string) bool
}

const (
	// SourceCore 表示由宿主核心注册的 hook。
	SourceCore = "core"
	// SourceTheme 表示由当前主题注册的 hook。
	SourceTheme = "theme"
	// SourcePlugin 表示由插件注册的 hook。
	SourcePlugin = "plugin"
)

// Handler 是注册到某个 action/filter 上的处理器。
type Handler struct {
	Name     string // 名称
	Priority int    // 优先级
	Source   Source // 来源
	Fn       any    // 函数实现
	order    int64  // 相同优先级时按添加顺序
}

var _ sort.Interface = (Handlers)(nil)

type Handlers []Handler

func (h Handlers) Len() int { return len(h) }

func (h Handlers) Less(i, j int) bool {
	if h[i].Priority != h[j].Priority {
		return h[i].Priority < h[j].Priority
	}
	return h[i].order < h[j].order
}

func (h Handlers) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

var _ Registry = (*Hooks)(nil)

// Hooks 保存所有 action/filter 处理器。
type Hooks struct {
	mu         sync.RWMutex
	actions    map[string]Handlers
	filters    map[string]Handlers
	didActions map[string]int
	next       int64
	log        *slog.Logger
}

// NewRegistry 创建一个空 Hook Registry。
func NewRegistry() *Hooks {
	return &Hooks{
		actions:    make(map[string]Handlers),
		filters:    make(map[string]Handlers),
		didActions: make(map[string]int),
		log:        slog.Default().With("component", "hooks"),
	}
}

// AddAction 注册 action 处理器。
func (r *Hooks) AddAction(name string, fn any, source Source, priority ...int) {
	if r == nil || name == "" || fn == nil {
		return
	}
	r.add(r.actions, name, fn, source, firstPriority(priority))
}

// AddFilter 注册 filter 处理器。
func (r *Hooks) AddFilter(name string, fn any, source Source, priority ...int) {
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

func (r *Hooks) add(target map[string]Handlers, name string, fn any, source Source, priority int) {
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
func (r *Hooks) RemoveAction(name string, source Source) int {
	if r == nil || name == "" {
		return 0
	}
	return r.remove(r.actions, name, source)
}

// RemoveFilter 移除指定 source 注册的所有同名 filter 处理器。
// 返回实际移除的数量。
func (r *Hooks) RemoveFilter(name string, source Source) int {
	if r == nil || name == "" {
		return 0
	}
	return r.remove(r.filters, name, source)
}

func (r *Hooks) remove(target map[string]Handlers, name string, source Source) int {
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
func (r *Hooks) Actions(name string) []Handler {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneHandlers(r.actions[name])
}

// Filters 返回指定 filter 当前注册的处理器快照。
func (r *Hooks) Filters(name string) []Handler {
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
func (r *Hooks) DoAction(ctx context.Context, name string, w io.Writer, args ...any) {
	if r == nil {
		return
	}
	ctx = WithActionWriter(ctx, w)
	ctx = WithCurrentHook(ctx, name)
	r.mu.Lock()
	r.didActions[name]++
	r.mu.Unlock()
	for _, h := range r.Actions(name) {
		r.safeDoAction(ctx, h, args...)
	}
}

func (r *Hooks) safeDoAction(ctx context.Context, h Handler, args ...any) {
	defer r.recoverHandler(ctx, h)
	switch fn := h.Fn.(type) {
	case func(context.Context, ...any):
		fn(ctx, args...)
	case func(...any):
		fn(args...)
	case func():
		fn()
	default:
		// 这里保留反射是为了兼容 yaegi 脚本里声明的具体函数签名。
		callByReflection(h.Fn, ctx, false, nil, args...)
	}
}

// ApplyFilters 依次执行 filter，并把上一个 filter 的返回值传给下一个 filter。
func (r *Hooks) ApplyFilters(ctx context.Context, name string, value any, args ...any) any {
	current := value
	for _, h := range r.Filters(name) {
		current = r.safeApplyFilter(ctx, h, current, args...)
	}
	return current
}

// DidAction 返回指定 action 已被触发的次数。
func (r *Hooks) DidAction(name string) int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.didActions[name]
}

// DoingAction 返回指定 action 当前是否正在执行中。
func (r *Hooks) DoingAction(ctx context.Context, name string) bool {
	return CurrentHook(ctx) == name
}

// ReplaceAllFrom 原子替换所有 hook 处理器，用于插件重载场景。
// 编译到临时 Registry 成功后调用此方法，避免替换 Registry 实例本身。
func (r *Hooks) ReplaceAllFrom(other *Hooks) {
	if r == nil {
		return
	}
	actions := make(map[string]Handlers)
	filters := make(map[string]Handlers)
	didActions := make(map[string]int)
	var next int64
	if other != nil {
		other.mu.RLock()
		actions = cloneHandlerMap(other.actions)
		filters = cloneHandlerMap(other.filters)
		for name, n := range other.didActions {
			didActions[name] = n
		}
		next = other.next
		other.mu.RUnlock()
	}
	r.mu.Lock()
	r.actions = actions
	r.filters = filters
	r.didActions = didActions
	r.next = next
	r.mu.Unlock()
}

func cloneHandlerMap(in map[string]Handlers) map[string]Handlers {
	out := make(map[string]Handlers, len(in))
	for name, handlers := range in {
		out[name] = cloneHandlers(handlers)
	}
	return out
}

func (r *Hooks) safeApplyFilter(ctx context.Context, h Handler, value any, args ...any) (out any) {
	out = value
	defer func() {
		if rec := recover(); rec != nil {
			r.logPanic(ctx, h, rec)
			out = value
		}
	}()
	switch fn := h.Fn.(type) {
	case func(context.Context, any, ...any) any:
		return fn(ctx, value, args...)
	case func(any, ...any) any:
		return fn(value, args...)
	case func(any) any:
		return fn(value)
	default:
		// 同 safeDoAction：filter 也允许插件/主题使用具体签名。
		result, ok := callByReflection(h.Fn, ctx, true, value, args...)
		if !ok {
			return value
		}
		return result
	}
}

func (r *Hooks) recoverHandler(ctx context.Context, h Handler) {
	if rec := recover(); rec != nil {
		r.logPanic(ctx, h, rec)
	}
}

func (r *Hooks) logPanic(ctx context.Context, h Handler, rec any) {
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

func callByReflection(fn any, ctx context.Context, includeValue bool, value any, args ...any) (any, bool) {
	rv := reflect.ValueOf(fn)
	if !rv.IsValid() || rv.Kind() != reflect.Func {
		return value, false
	}
	rt := rv.Type()
	values := make([]any, 0, len(args)+2)
	if shouldInjectContext(rt) {
		values = append(values, ctx)
	}
	if includeValue {
		values = append(values, value)
	}
	values = append(values, args...)

	in := buildReflectArgs(rt, values)
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

var contextType = reflect.TypeOf((*context.Context)(nil)).Elem()

func shouldInjectContext(rt reflect.Type) bool {
	if rt.NumIn() == 0 {
		return false
	}
	if rt.IsVariadic() && rt.NumIn() == 1 {
		return false
	}
	first := rt.In(0)
	return first == contextType || (first.Kind() == reflect.Interface && first.NumMethod() > 0 && contextType.Implements(first))
}

func buildReflectArgs(rt reflect.Type, values []any) []reflect.Value {
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
