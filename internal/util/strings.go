package util

import (
	"os"
	"strings"

	"github.com/youthlin/blog/internal/consts"
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
