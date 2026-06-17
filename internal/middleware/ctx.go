package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
)

func ResetGinCtx(c *gin.Context, fn func(ctx context.Context) context.Context) {
	ctx := c.Request.Context()
	ctx = fn(ctx)
	c.Request = c.Request.WithContext(ctx)
}
