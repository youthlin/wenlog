// Package middleware — SQL 追踪中间件。
package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/store"
)

// SQLTracer 在请求前向 context 注入 SQLDetails 收集器，
// 请求结束后将 SQL 执行详情输出到日志。
func SQLTracer(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		details := &store.SQLDetails{}
		ctx := store.CtxWithSQLDetails(c.Request.Context(), details)
		c.Request = c.Request.WithContext(ctx)
		// 同时设到 gin.Context 上，因为 handler 传 c（gin.Context）给 store，
		// GORM 插件里 db.Statement.Context 就是 *gin.Context，
		// CtxGetSQLDetails 会通过 string key 从 gin.Keys 中获取。
		c.Set(store.GinKeySQLDetails, details)
		c.Next()
		if formatted := store.FormatSQLDetails(details); formatted != "" {
			log.Info("sql details",
				slog.String("trace_id", GetTraceID(c.Request.Context())),
				slog.String("method", c.Request.Method),
				slog.String("path", c.Request.URL.Path),
				slog.String("sql", "\n"+formatted),
			)
		}
	}
}
