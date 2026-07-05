//go:build !unix || ios

package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/youthlin/wenlog/internal/config"
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
		case "restart":
			fmt.Fprintf(os.Stderr, "重启服务失败, err=%v\n", restartDaemon(cfg))
			os.Exit(1)
		}
	}
	return false
}

func writePidFile(cfg *config.Config) func() {
	return func() {}
}

func startDaemon(cfg *config.Config) error {
	_ = cfg
	return fmt.Errorf("当前平台 %s 暂不支持 start/stop 后台模式；请直接前台运行，或交给系统服务管理器托管", runtime.GOOS)
}

func stopDaemon(cfg *config.Config) error {
	_ = cfg
	return fmt.Errorf("当前平台 %s 暂不支持 start/stop/restart 后台模式；请直接前台运行，或交给系统服务管理器托管", runtime.GOOS)
}

func restartDaemon(cfg *config.Config) error {
	_ = cfg
	return fmt.Errorf("当前平台 %s 暂不支持 start/stop/restart 后台模式；请直接前台运行，或交给系统服务管理器托管", runtime.GOOS)
}
