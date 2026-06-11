package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/store"
)

func requestBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if v := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			v = v[:i]
		}
		scheme = strings.TrimSpace(v)
	}
	host := strings.TrimSpace(c.Request.Host)
	if v := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			v = v[:i]
		}
		host = strings.TrimSpace(v)
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func positiveIntSetting(raw string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return def
	}
	return n
}

func syncPostPermalink(st *store.Store) string {
	pattern := consts.SettingsPostPermalinkDefault
	categoryPrefix := consts.SettingsCategoryPrefixDefault
	tagPrefix := consts.SettingsTagPrefixDefault
	if st != nil {
		if v, err := st.GetSetting(consts.SettingsPostPermalink); err == nil && strings.TrimSpace(v) != "" {
			pattern = v
		}
		if v, err := st.GetSetting(consts.SettingsCategoryPrefix); err == nil && strings.TrimSpace(v) != "" {
			categoryPrefix = v
		}
		if v, err := st.GetSetting(consts.SettingsTagPrefix); err == nil && strings.TrimSpace(v) != "" {
			tagPrefix = v
		}
	}
	permalink.SetPostPattern(pattern)
	permalink.SetTaxonomyPrefixes(categoryPrefix, tagPrefix)
	return permalink.CurrentPostPattern()
}
