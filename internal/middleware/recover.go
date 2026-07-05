package middleware

import (
	"log/slog"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recover gin中间件, 捕获 panic,记录日志并返回 500。
func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(c, "panic recovered",
					slog.Any("error", r),
					slog.String("stack", string(debug.Stack())),
				)
				c.AbortWithStatus(500)
			}
		}()
		c.Next()
	}
}
