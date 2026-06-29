package hook

import (
	"context"

	"github.com/youthlin/blog/internal/store"
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
