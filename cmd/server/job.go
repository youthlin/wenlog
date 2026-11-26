package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/email"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/util"
)

func startCronJob(st *store.Store) func() {
	ctx, cancel := context.WithCancel(context.Background())
	// 定时发布 goroutine：每分钟检查一次
	publishScheduled(ctx, st)
	// 自动备份 goroutine：按后台设置每天执行，默认凌晨 3 点。
	autoBackupDB(ctx, st)
	// 浏览量批量落库：每 1 分钟 flush 一次内存中的浏览量增量。
	flushViews(ctx, st)
	return cancel
}

func flushViews(ctx context.Context, st *store.Store) {
	util.Go(ctx, func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := st.FlushViews(ctx); err != nil {
					slog.ErrorContext(ctx, "浏览量批量落库失败", slog.Any("error", err))
				}
			case <-ctx.Done():
				// 退出前再 flush 一次，尽量不丢数据
				shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := st.FlushViews(shutdownCtx); err != nil {
					slog.ErrorContext(shutdownCtx, "退出时浏览量落库失败", slog.Any("error", err))
				}
				return
			}
		}
	})
}

func publishScheduled(ctx context.Context, st *store.Store) {
	util.Go(ctx, func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n, err := st.PublishScheduled(ctx)
				if err != nil {
					slog.ErrorContext(bg, "检查定时发布文章失败", slog.Any("error", err))
				} else if n > 0 {
					slog.InfoContext(bg, "检查定时发布文章成功", slog.Int64("count", n))
				}
			case <-ctx.Done():
				return
			}
		}
	})
}

func autoBackupDB(ctx context.Context, st *store.Store) {
	util.Go(ctx, func() {
		for {
			enabled, hour, minute, keep := autoBackupSettings(ctx, st)
			if !enabled {
				slog.InfoContext(ctx, "定时自动备份数据库已关闭")
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
			slog.InfoContext(ctx, "下次定时自动备份数据库", slog.Time("at", next), slog.Duration("in", d))
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
				slog.ErrorContext(ctx, "备份数据库失败", slog.Any("error", err))
			} else {
				slog.InfoContext(ctx, "备份数据库成功", slog.String("path", path))
				if err := sendAutoBackupEmail(ctx, st, path); err != nil {
					slog.ErrorContext(ctx, "自动备份邮件发送失败，本地备份已保留", slog.String("path", path), slog.Any("error", err))
				}
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

// sendAutoBackupEmail 在备份完成后读取最新邮件设置；发送不影响本地备份保留。
func sendAutoBackupEmail(ctx context.Context, st *store.Store, path string) error {
	settings, err := st.GetSettings(ctx, consts.SettingsAutoBackupEmailEnabled, consts.SettingsAutoBackupEmailTo,
		consts.SettingsSMTPHost, consts.SettingsSMTPPort, consts.SettingsSMTPUser, consts.SettingsSMTPPassword,
		consts.SettingsSMTPFrom, consts.SettingsSiteName, consts.SettingsSiteURL)
	if err != nil {
		return err
	}
	if !strings.EqualFold(settings[consts.SettingsAutoBackupEmailEnabled], "true") {
		return nil
	}
	to := strings.TrimSpace(settings[consts.SettingsAutoBackupEmailTo])
	address, err := mail.ParseAddress(to)
	if err != nil || address.Address != to || strings.ContainsAny(to, "\r\n") {
		return fmt.Errorf("无效的自动备份收件人邮箱")
	}
	cfg := email.ConfigFromSettings(settings)
	if !cfg.Configured() {
		return fmt.Errorf("SMTP 未配置")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取备份文件失败: %w", err)
	}
	siteName := strings.TrimSpace(settings[consts.SettingsSiteName])
	if siteName == "" {
		siteName = consts.SettingsSiteNameDefault
	}
	now := time.Now()
	subject := fmt.Sprintf("%s - 数据库备份 %s", siteName, now.Format("2006-01-02 15:04"))
	body := fmt.Sprintf("这是 %s 的数据库备份文件，备份时间：%s。", siteName, now.Format("2006-01-02 15:04:05"))
	if siteURL := strings.TrimSpace(settings[consts.SettingsSiteURL]); siteURL != "" {
		body += "\n\n" + siteURL
	}
	if err := cfg.SendWithAttachment(to, subject, body, filepath.Base(path), data); err != nil {
		return err
	}
	slog.InfoContext(ctx, "自动备份邮件发送成功", slog.String("path", path))
	return nil
}
