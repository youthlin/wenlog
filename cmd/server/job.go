package main

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/util"
)

func startCronJob(st *store.Store) func() {
	ctx, cancel := context.WithCancel(context.Background())
	// 定时发布 goroutine：每分钟检查一次
	publishScheduled(ctx, st)
	// 自动备份 goroutine：按后台设置每天执行，默认凌晨 3 点。
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
			enabled, hour, minute, keep := autoBackupSettings(ctx, st)
			if !enabled {
				slog.Info("定时自动备份数据库已关闭")
				select {
				case <-time.After(1 * time.Hour):
				case <-ctx.Done():
					return
				}
				continue
			}
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
			if now.After(next) {
				next = next.Add(24 * time.Hour)
			}
			d := next.Sub(now)
			slog.Info("下次定时自动备份数据库", slog.Time("at", next), slog.Duration("in", d))
			wait := d
			if wait > time.Hour {
				wait = time.Hour
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return
			}
			if wait < d {
				continue
			}
			path, err := st.BackupDB()
			if err != nil {
				slog.Error("备份数据库失败", slog.Any("error", err))
			} else {
				slog.Info("备份数据库成功", slog.String("path", path))
				_ = st.CleanOldBackups(keep)
			}
		}
	})
}

func autoBackupSettings(ctx context.Context, st *store.Store) (enabled bool, hour int, minute int, keep int) {
	enabled = true
	hour = 3
	minute = 0
	keep = consts.SettingsAutoBackupKeepDefault
	settings, err := st.GetSettings(ctx, consts.SettingsAutoBackupEnabled, consts.SettingsAutoBackupTime, consts.SettingsAutoBackupKeep)
	if err != nil {
		return enabled, hour, minute, keep
	}
	if strings.EqualFold(settings[consts.SettingsAutoBackupEnabled], "false") {
		enabled = false
	}
	if h, m, ok := parseAutoBackupTime(settings[consts.SettingsAutoBackupTime]); ok {
		hour, minute = h, m
	}
	if n, err := strconv.Atoi(settings[consts.SettingsAutoBackupKeep]); err == nil && n > 0 {
		keep = n
	}
	return enabled, hour, minute, keep
}

func parseAutoBackupTime(value string) (hour int, minute int, ok bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, false
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}
