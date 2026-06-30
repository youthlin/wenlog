package hook

import "context"

// Provider 是 Hook Registry 的最小接口，render 包只需要 DoAction 和 ApplyFilters。
type Provider interface {
	DoAction(ctx context.Context, name string, args ...any)
	ApplyFilters(ctx context.Context, name string, value any, args ...any) any
}
type ProviderGetter = func() Provider

// Registry 是 hook 注册与执行接口。
// theme 包通过此接口使用 hook 能力，plugin 包的 Registry 实现此接口。
type Registry interface {
	Provider
	AddAction(name string, fn any, source Source, priority ...int)
	AddFilter(name string, fn any, source Source, priority ...int)
	RemoveAction(name string, source Source) int
	RemoveFilter(name string, source Source) int
	DidAction(name string) int
	DoingAction(ctx context.Context, name string) bool
}
