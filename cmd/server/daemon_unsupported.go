//go:build !unix || android || ios

package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"github.com/youthlin/blog/internal/config"
)

func runDaemon(cfg *config.Config) bool {
	_ = cfg
	switch len(flag.Args()) {
	case 0:
		return false
	default:
		switch flag.Args()[0] {
		case "start":
			fmt.Fprintf(os.Stderr, "后台启动失败, err=%v\n", startDaemon(cfg))
			os.Exit(1)
		case "stop":
			fmt.Fprintf(os.Stderr, "停止服务失败, err=%v\n", stopDaemon(cfg))
			os.Exit(1)
		}
	}
	return false
}

func writePidFile(cfg *config.Config, log *slog.Logger) func() {
	return func() {}
}

func startDaemon(cfg *config.Config) error {
	_ = cfg
	return fmt.Errorf("当前平台 %s 暂不支持 start/stop 后台模式；请直接前台运行，或交给系统服务管理器托管", runtime.GOOS)
}

func stopDaemon(cfg *config.Config) error {
	_ = cfg
	return fmt.Errorf("当前平台 %s 暂不支持 start/stop 后台模式；请直接前台运行，或交给系统服务管理器托管", runtime.GOOS)
}

func processExists(pid int) bool {
	_ = pid
	return false
}
