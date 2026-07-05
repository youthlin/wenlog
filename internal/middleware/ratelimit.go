package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter 频率限制接口,预留后续迁移 Redis 等实现。
type RateLimiter interface {
	// Allow 检查 key 在 window 时间窗口内是否超过 max 次。返回 true 表示允许。
	Allow(key string, window time.Duration, max int) bool
	// Reset 清除指定 key 的记录。
	Reset(key string)
}

// memoryRateLimiter 基于内存的频率限制实现。
type memoryRateLimiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
}

// NewMemoryRateLimiter 创建基于内存的频率限制器。
func NewMemoryRateLimiter() RateLimiter {
	limiter := &memoryRateLimiter{
		buckets: make(map[string][]time.Time),
	}
	// 定期清理过期记录。
	go limiter.cleanupLoop()
	return limiter
}

func (m *memoryRateLimiter) Allow(key string, window time.Duration, max int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)
	times := m.buckets[key]

	// 移除窗口外的记录。
	valid := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) >= max {
		m.buckets[key] = valid
		return false
	}
	valid = append(valid, now)
	m.buckets[key] = valid
	return true
}

func (m *memoryRateLimiter) Reset(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.buckets, key)
}

func (m *memoryRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.cleanup()
	}
}

func (m *memoryRateLimiter) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-30 * time.Minute)
	for key, times := range m.buckets {
		valid := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(m.buckets, key)
		} else {
			m.buckets[key] = valid
		}
	}
}

// RateLimitConfig 频率限制配置。
type RateLimitConfig struct {
	Window  time.Duration // 时间窗口
	Max     int           // 窗口内最大请求数
	KeyFunc func(c *gin.Context) string
}

// DefaultRateLimitKey 默认按客户端 IP 限频。
func DefaultRateLimitKey(c *gin.Context) string {
	return c.ClientIP()
}

// RateLimitMiddleware 返回频率限制中间件。
func RateLimitMiddleware(limiter RateLimiter, cfg RateLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := cfg.KeyFunc(c)
		if !limiter.Allow(key, cfg.Window, cfg.Max) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "请求过于频繁，请稍后再试。",
			})
			return
		}
		c.Next()
	}
}
