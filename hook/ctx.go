package hook

import (
	"context"
	"io"

	"github.com/youthlin/wenlog/internal/store"
)

type loaderContextKey struct{}

// WithDataLoader 把当前请求的只读数据加载器绑定到 context，供插件 API 读取。
func WithDataLoader(ctx context.Context, loader *store.DataLoader) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if loader == nil {
		return ctx
	}
	return context.WithValue(ctx, loaderContextKey{}, loader)
}

func getDataLoader(ctx context.Context) *store.DataLoader {
	loader, _ := ctx.Value(loaderContextKey{}).(*store.DataLoader)
	return loader
}

// actionWriterKey 用于在 context 中存储当前 action 的输出 writer。
type actionWriterKey struct{}

// WithActionWriter 把输出 writer 注入 context，供 api.Printf 使用。
func WithActionWriter(ctx context.Context, w io.Writer) context.Context {
	return context.WithValue(ctx, actionWriterKey{}, w)
}

// GetActionWriter 从 context 中取出当前 action 的输出 writer。
func GetActionWriter(ctx context.Context) io.Writer {
	if w, ok := ctx.Value(actionWriterKey{}).(io.Writer); ok {
		return w
	}
	return nil
}

// currentHookKey 用于在 context 中存储当前正在执行的 hook 名称。
type currentHookKey struct{}

// WithCurrentHook 把当前 hook 名注入 context。
func WithCurrentHook(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, currentHookKey{}, name)
}

// CurrentHook 从 context 中读取当前正在执行的 hook 名称。
func CurrentHook(ctx context.Context) string {
	if s, ok := ctx.Value(currentHookKey{}).(string); ok {
		return s
	}
	return ""
}
