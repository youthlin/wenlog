// Package middleware — SQL 追踪中间件。
package middleware

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/store"
)

// SQLTracer 在请求前向 context 注入 SQLDetails 收集器，
// 请求结束后将 SQL 执行详情输出到日志。
func SQLTracer() gin.HandlerFunc {
	return func(c *gin.Context) {
		details := &store.SQLDetails{
			TraceID: GetTraceID(c),
		}
		ResetGinCtx(c, func(ctx context.Context) context.Context {
			ctx = store.CtxWithSQLDetails(ctx, details)
			return ctx
		})
		c.Next()
		if formatted := store.FormatSQLDetails(details); formatted != "" {
			slog.InfoContext(c, "sql details", slog.String("sql", formatted))
		}
	}
}
