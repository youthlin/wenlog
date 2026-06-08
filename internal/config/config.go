// Package config 负责从环境变量加载运行时配置,并提供合理默认值。
package config

import (
	"os"
	"strconv"
)

// Config 是应用运行时配置。
type Config struct {
	// Addr 是 HTTP 监听地址,如 ":8080"。
	Addr string
	// DBPath 是 SQLite 数据库文件路径。
	DBPath string
	// PublicDir 是历史图片等静态资源目录(/wp-content/uploads 即在其下)。
	PublicDir string
	// LogJSON 为 true 时输出 JSON 结构化日志,否则文本。
	LogJSON bool
}

// Load 从环境变量读取配置,缺省时使用默认值。
func Load() *Config {
	return &Config{
		Addr:      env("BLOG_ADDR", ":8888"),
		DBPath:    env("BLOG_DB", "data/blog.db"),
		PublicDir: env("BLOG_PUBLIC_DIR", "public"),
		LogJSON:   envBool("BLOG_LOG_JSON", false),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}
