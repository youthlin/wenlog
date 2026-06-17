package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger gin 中间件 输出结构化访问日志。
func Logger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ResetGinCtx(c, func(ctx context.Context) context.Context {
			traceID := GetTraceID(ctx)
			ctx = CtxAddAttr(ctx,
				slog.String("method", c.Request.Method),
				slog.String("path", c.Request.URL.Path),
				slog.String("ip", c.ClientIP()),
				slog.String("trace_id", traceID),
			)
			return ctx
		})
		start := time.Now()
		c.Next()
		log.InfoContext(c, "http request done",
			slog.Int("status", c.Writer.Status()),
			slog.Duration("latency", time.Since(start)),
		)
	}
}

type ctxLoggerKey struct{}

// CtxAddAttr 往 ctx 上附加日志字段
func CtxAddAttr(ctx context.Context, attrs ...slog.Attr) context.Context {
	pre := CtxAttrs(ctx)
	attrs = append(pre, attrs...)
	return context.WithValue(ctx, ctxLoggerKey{}, attrs)
}

// CtxAttrs 从 ctx 中取出附加的日志字段
func CtxAttrs(ctx context.Context) []slog.Attr {
	if v, ok := ctx.Value(ctxLoggerKey{}).([]slog.Attr); ok {
		return v
	}
	return nil
}

// CtxLoggerHandler 自定义的 [slog.Handler] 会在打日志时自动从 ctx 收集通过 [CtxAddAttr] 附加的日志字段
type CtxLoggerHandler struct {
	inner slog.Handler
}

var _ slog.Handler = (*CtxLoggerHandler)(nil)

func NewCtxLoggerHandler(h slog.Handler) *CtxLoggerHandler {
	return &CtxLoggerHandler{
		inner: h,
	}
}

// Enabled implements [slog.Handler].
func (c *CtxLoggerHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return c.inner.Enabled(ctx, level)
}

// Handle implements [slog.Handler].
func (c *CtxLoggerHandler) Handle(ctx context.Context, r slog.Record) error {
	// 从 ctx 取出注入的 attrs
	if attrs := CtxAttrs(ctx); len(attrs) > 0 {
		r = r.Clone()
		r.AddAttrs(attrs...)
		return c.inner.Handle(ctx, r)
	}
	return c.inner.Handle(ctx, r)
}

// WithAttrs implements [slog.Handler].
func (c *CtxLoggerHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CtxLoggerHandler{
		inner: c.inner.WithAttrs(attrs),
	}
}

// WithGroup implements [slog.Handler].
func (c *CtxLoggerHandler) WithGroup(name string) slog.Handler {
	return &CtxLoggerHandler{
		inner: c.inner.WithGroup(name),
	}
}
