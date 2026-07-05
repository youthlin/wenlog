package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"

	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/email"
	"github.com/youthlin/wenlog/internal/store"
)

// randomHexToken 生成 n 字节随机 hex 令牌。
func randomHexToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// siteNameFromStore 从设置中读取站点名称。
func siteNameFromStore(ctx context.Context, st *store.Store) string {
	settings, _ := st.GetSettings(ctx, consts.SettingsSiteName)
	if name := strings.TrimSpace(settings[consts.SettingsSiteName]); name != "" {
		return name
	}
	return consts.SettingsSiteNameDefault
}

// smtpConfigFromStore 从设置中读取 SMTP 配置。
func smtpConfigFromStore(ctx context.Context, st *store.Store) email.Config {
	settings, _ := st.GetSettings(ctx, consts.SettingsSMTPHost,
		consts.SettingsSMTPPort,
		consts.SettingsSMTPUser,
		consts.SettingsSMTPPassword,
		consts.SettingsSMTPFrom,
	)
	return smtpConfigFromSettings(settings)
}

func smtpConfigFromSettings(settings map[string]string) email.Config {
	port := 0
	if p := settings[consts.SettingsSMTPPort]; p != "" {
		port, _ = strconv.Atoi(p)
	}
	return email.Config{
		Host:     settings[consts.SettingsSMTPHost],
		Port:     port,
		User:     settings[consts.SettingsSMTPUser],
		Password: settings[consts.SettingsSMTPPassword],
		From:     settings[consts.SettingsSMTPFrom],
	}
}

func configuredSiteURL(ctx context.Context, st *store.Store) (string, bool) {
	settings, _ := st.GetSettings(ctx, consts.SettingsSiteURL)
	u := strings.TrimSpace(settings[consts.SettingsSiteURL])
	if u == "" {
		return "", false
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	return strings.TrimRight(u, "/"), true
}

func siteURLFromRequest(st *store.Store, c *gin.Context) string {
	if u, ok := configuredSiteURL(c, st); ok {
		return u
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

func mailBodyWithSiteDomain(tr *gettext.Translations, body, siteURL string) string {
	domain := siteDomain(siteURL)
	if domain == "" {
		return body
	}
	body = strings.TrimRight(body, "\r\n")
	if tr == nil {
		return body + "\n\n站点域名：" + domain + "\n"
	}
	return body + tr.T("\n\n站点域名：%s\n", domain)
}

func siteDomain(siteURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(siteURL))
	if err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return ""
}
