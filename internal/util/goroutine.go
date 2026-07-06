package util

import (
	"context"
	"log/slog"
	"runtime/debug"
)

func Go(ctx context.Context, fn func()) {
	go func() {
		defer func() {
			if x := recover(); x != nil {
				slog.ErrorContext(ctx, "发生了panic!", slog.String("stack", string(debug.Stack())))
			}
		}()
		fn()
	}()
}
