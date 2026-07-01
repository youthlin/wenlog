package util

import (
	"log/slog"
	"runtime/debug"
)

func Go(fn func()) {
	go func() {
		defer func() {
			if x := recover(); x != nil {
				slog.Error("发生了panic!", slog.String("stack", string(debug.Stack())))
			}
		}()
		fn()
	}()
}
