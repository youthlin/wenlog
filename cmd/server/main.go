// Command server 启动博客 HTTP 服务。
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/youthlin/wenlog/internal/config"
	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/i18n"
	"github.com/youthlin/wenlog/internal/middleware"
	"github.com/youthlin/wenlog/internal/store"
)

var bg = context.Background()

func main() {
	// 解析命令行参数
	flag.Parse()
	var cfg = config.Load()
	setDefaultLogger(cfg.LogJSON)

	// 是否后台运行
	if runDaemon(cfg) {
		return
	}

	// 初始化
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		slog.ErrorContext(bg, "初始化数据库失败", slog.Any("error", err))
		os.Exit(1)
	}
	// 从 DB 加载日志级别配置
	if savedLevel, err := st.GetSetting(bg, consts.SettingsLogLevel); err == nil && savedLevel != "" {
		if middleware.SetLogLevel(savedLevel) {
			slog.Info("log level loaded from db", "level", savedLevel)
		}
	}
	if err = i18n.Init(); err != nil {
		slog.ErrorContext(bg, "加载i18n资源失败", slog.Any("error", err))
		os.Exit(1)
	}

	// 重置密码功能
	if setPasswd(st) {
		return
	}
	// 自动创建管理员
	if err = ensureInitialAdmin(st); err != nil {
		slog.ErrorContext(bg, "自动创建管理员账号失败", slog.Any("error", err))
		os.Exit(1)
	}
	// 自动创建初始内容
	if err = ensureInitialContent(st); err != nil {
		slog.ErrorContext(bg, "自动插入初始内容失败", slog.Any("error", err))
		os.Exit(1)
	}

	// 在监听前写入 pid 文件, 退出时删除
	defer writePidFile(cfg)()
	// 启动定时任务
	defer startCronJob(st)()
	// 启动 web 服务器监听
	if err := serve(cfg, st); err != nil {
		slog.ErrorContext(bg, "服务监听失败", slog.Any("error", err))
		os.Exit(1)
	}
}

func setDefaultLogger(jsonOut bool) {
	middleware.SetLogLevel("info")
	opts := &slog.HandlerOptions{
		AddSource: true,
		Level:     middleware.LogLevel,
	}
	var h slog.Handler
	if jsonOut {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	h = middleware.NewCtxLoggerHandler(h)
	slog.SetDefault(slog.New(h))
}
