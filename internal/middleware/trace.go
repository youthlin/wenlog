// Package middleware — Trace ID 中间件。
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

type traceIDKey struct{}

// TraceID 为每个请求生成唯一 trace ID，存入 context 和 gin.Context，
// 设置 X-Trace-Id 响应头，并注入到日志属性中。
func TraceID() gin.HandlerFunc {
	return func(c *gin.Context) {
		var traceID = generateTraceID()
		ResetGinCtx(c, func(ctx context.Context) context.Context {
			ctx = context.WithValue(ctx, traceIDKey{}, traceID)
			return ctx
		})
		c.Set("TraceID", traceID)
		c.Header("X-Trace-Id", traceID)
		c.Next()
	}
}

// GetTraceID 从 context 中取出 trace ID。
func GetTraceID(ctx context.Context) string {
	if v := ctx.Value(traceIDKey{}); v != nil {
		return v.(string)
	}
	return ""
}

func generateTraceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下回退，不影响请求处理
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}
