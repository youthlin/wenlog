package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"
)

func startCronJob(st *store.Store) func() {
	ctx, cancel := context.WithCancel(context.Background())
	// 定时发布 goroutine：每分钟检查一次
	publishScheduled(ctx, st)
	// 自动备份 goroutine：每天凌晨 3 点执行
	autoBackupDB(ctx, st)
	return cancel
}

func publishScheduled(ctx context.Context, st *store.Store) {
	util.Go(func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n, err := st.PublishScheduled(ctx)
				if err != nil {
					slog.Error("检查定时发布文章失败", slog.Any("error", err))
				} else if n > 0 {
					slog.Info("检查定时发布文章成功", slog.Int64("count", n))
				}
			case <-ctx.Done():
				return
			}
		}
	})
}

func autoBackupDB(ctx context.Context, st *store.Store) {
	util.Go(func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
			if now.After(next) {
				next = next.Add(24 * time.Hour)
			}
			d := next.Sub(now)
			slog.Info("下次定时自动备份数据库", slog.Time("at", next), slog.Duration("in", d))
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return
			}
			path, err := st.BackupDB()
			if err != nil {
				slog.Error("备份数据库失败", slog.Any("error", err))
			} else {
				slog.Info("备份数据库成功", slog.String("path", path))
				_ = st.CleanOldBackups(10)
			}
		}
	})
}
