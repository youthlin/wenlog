// Command server 启动博客 HTTP 服务。
package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/youthlin/blog/internal/config"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/store"
)

func main() {
	// 解析命令行参数
	flag.Parse()
	var cfg = config.Load()

	// 是否后台运行
	if runDaemon(cfg) {
		return
	}

	// 初始化
	var (
		log     = newLogger(cfg.LogJSON)
		st, err = store.Open(cfg.DBPath)
	)
	if err != nil {
		log.Error("open store", slog.Any("error", err))
		os.Exit(1)
	}
	if err = i18n.Init(); err != nil {
		log.Error("init i18n", slog.Any("error", err))
		os.Exit(1)
	}

	// 重置密码功能
	if setPasswd(st) {
		return
	}
	// 自动创建管理员
	if err = ensureInitialAdmin(st); err != nil {
		log.Error("ensure initial admin", slog.Any("error", err))
		os.Exit(1)
	}
	// 自动创建初始内容
	if err = ensureInitialContent(st); err != nil {
		log.Error("ensure initial content", slog.Any("error", err))
		os.Exit(1)
	}

	// 在监听前写入 pid 文件, 退出时删除
	defer writePidFile(cfg, log)()
	// 启动 web 服务器监听
	if err := serve(cfg, log, st); err != nil {
		log.Error("serve", slog.Any("error", err))
		os.Exit(1)
	}
}

func newLogger(jsonOut bool) *slog.Logger {
	opts := &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}
	var h slog.Handler
	if jsonOut {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	h = middleware.NewCtxLoggerHandler(h)
	return slog.New(h)
}
