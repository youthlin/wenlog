package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"html/template"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/skip2/go-qrcode"
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

const (
	totpSecretBytes = 20
	totpPeriod      = 30
	totpDigits      = 6
)

func newTOTPSecret() (string, error) {
	b := make([]byte, totpSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func normalizeTOTPSecret(secret string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, secret))
}

func validTOTPSecret(secret string) bool {
	_, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalizeTOTPSecret(secret))
	return err == nil
}

func verifyTOTPCode(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalizeTOTPSecret(secret))
	if err != nil {
		return false
	}
	counter := now.Unix() / totpPeriod
	for offset := int64(-1); offset <= 1; offset++ {
		if subtle.ConstantTimeCompare([]byte(totpCode(key, uint64(counter+offset))), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

func totpCode(key []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		(uint32(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%0*d", totpDigits, bin%uint32(math.Pow10(totpDigits)))
}

func totpSetupURL(siteName, username, secret string) string {
	issuer := strings.TrimSpace(siteName)
	if issuer == "" {
		issuer = consts.SettingsSiteNameDefault
	}
	account := strings.TrimSpace(username)
	u := url.URL{Scheme: "otpauth", Host: "totp", Path: "/" + issuer + ":" + account}
	q := u.Query()
	q.Set("secret", normalizeTOTPSecret(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(totpDigits))
	q.Set("period", strconv.Itoa(totpPeriod))
	u.RawQuery = q.Encode()
	return u.String()
}

func totpQRCodeDataURI(setupURL string) (template.URL, error) {
	png, err := qrcode.Encode(setupURL, qrcode.Medium, 180)
	if err != nil {
		return "", err
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)), nil //nolint:gosec // PNG data URI is generated from local QR bytes only.
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
