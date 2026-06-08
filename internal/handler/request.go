package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
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
