// Package middleware 提供 gin 中间件:结构化访问日志、prometheus 指标、
// panic 恢复。
package middleware

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"
)

var (
	reqTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "blog_http_requests_total",
		Help: "HTTP 请求总数",
	}, []string{"method", "path", "status"})

	reqDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "blog_http_request_duration_seconds",
		Help:    "HTTP 请求耗时(秒)",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path", "status"})
)

// Metrics 记录 prometheus 指标。route 用 gin 的 FullPath 以避免高基数。
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		reqTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		reqDuration.WithLabelValues(c.Request.Method, path, status).
			Observe(time.Since(start).Seconds())
	}
}

// MetricsBasicAuth 用固定用户名 metrics 和后台设置的密码保护 /metrics。
func MetricsBasicAuth(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		password := metricsPassword(c.Request.Context(), st)
		user, pass, ok := c.Request.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte("metrics")) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			c.Header("WWW-Authenticate", `Basic realm="metrics"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}

func metricsPassword(ctx context.Context, st *store.Store) string {
	if st == nil {
		return util.GenerateRandomString(32)
	}
	password, _ := st.GetSetting(ctx, consts.SettingsMetricsAuthPassword)
	password = strings.TrimSpace(password)
	if password != "" {
		return password
	}
	password = util.GenerateRandomString(24, util.WithAlphaNumer())
	_ = st.SetSetting(ctx, consts.SettingsMetricsAuthPassword, password)
	return password
}

// Logger 输出结构化访问日志。
func Logger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("http request done",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.String("ip", c.ClientIP()),
			slog.Duration("latency", time.Since(start)),
		)
	}
}

// Recover 捕获 panic,记录日志并返回 500。
func Recover(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered",
					slog.Any("error", r),
					slog.String("path", c.Request.URL.Path),
					slog.String("stack", string(debug.Stack())),
				)
				c.AbortWithStatus(500)
			}
		}()
		c.Next()
	}
}
