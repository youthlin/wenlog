package util

import (
	"io"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/youthlin/wenlog/internal/consts"
)

// FirstNonEmpty 返回第一个非空字符串,若全为空则返回空字符串。
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// FirstNonEmptyOr 返回第一个非空字符串,若全为空则返回 fallback。
func FirstNonEmptyOr(fallback string, values ...string) string {
	if v := FirstNonEmpty(values...); v != "" {
		return v
	}
	return fallback
}

// NormalizeDefaultAvatar 标准化默认头像类型,非法值返回默认值。
func NormalizeDefaultAvatar(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "mp", "blank", "cravatar", "identicon", "wavatar", "monsterid", "retro", "robohash":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return consts.SettingsDefaultAvatarDefault
	}
}

// PathExists 检查路径是否存在。
func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Slugify 将字符串转为 URL 友好的 slug:小写、空白转连字符、保留字母数字与 -_。
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = s
	}
	return slug
}

// URLSlugify 将字符串 slugify 后再做 URL 路径编码；英文保持原样，中文等非 ASCII 字符转为 %xx。
func URLSlugify(s string) string {
	if decoded, err := url.PathUnescape(strings.TrimSpace(s)); err == nil {
		s = decoded
	}
	return strings.ToLower(url.PathEscape(Slugify(s)))
}

// WriteString 向 io.StringWriter 写入字符串，忽略错误。
// 适用于 strings.Builder 等不会返回错误的写入场景，消除 linter 未处理 err 警告。
func WriteString(w io.StringWriter, s string) {
	if w != nil {
		_, _ = w.WriteString(s)
	}
}
