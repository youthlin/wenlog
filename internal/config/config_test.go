package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// 清除相关环境变量，确保测试默认值
	for _, key := range []string{"WENLOG_ADDR", "WENLOG_DB", "WENLOG_PUBLIC_DIR", "WENLOG_LOG_JSON"} {
		os.Unsetenv(key)
	}
	cfg := Load()
	if cfg.Addr != ":8888" {
		t.Errorf("Addr = %q, want :8888", cfg.Addr)
	}
	if cfg.DBPath != "data/wenlog.db" {
		t.Errorf("DBPath = %q, want data/wenlog.db", cfg.DBPath)
	}
	if cfg.PublicDir != "public" {
		t.Errorf("PublicDir = %q, want public", cfg.PublicDir)
	}
	if cfg.LogJSON != false {
		t.Errorf("LogJSON = %v, want false", cfg.LogJSON)
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("WENLOG_ADDR", ":9090")
	os.Setenv("WENLOG_DB", "/tmp/test.db")
	os.Setenv("WENLOG_PUBLIC_DIR", "/var/www/public")
	os.Setenv("WENLOG_LOG_JSON", "true")
	defer func() {
		for _, key := range []string{"WENLOG_ADDR", "WENLOG_DB", "WENLOG_PUBLIC_DIR", "WENLOG_LOG_JSON"} {
			os.Unsetenv(key)
		}
	}()

	cfg := Load()
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q, want :9090", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath = %q, want /tmp/test.db", cfg.DBPath)
	}
	if cfg.PublicDir != "/var/www/public" {
		t.Errorf("PublicDir = %q, want /var/www/public", cfg.PublicDir)
	}
	if cfg.LogJSON != true {
		t.Errorf("LogJSON = %v, want true", cfg.LogJSON)
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		envValue string
		def      bool
		want     bool
	}{
		{"true", false, true},
		{"TRUE", false, true},
		{"1", false, true},
		{"false", true, false},
		{"FALSE", true, false},
		{"0", true, false},
		{"invalid", true, true},   // 解析失败回退默认值
		{"invalid", false, false}, // 解析失败回退默认值
		{"", true, true},          // 空字符串回退默认值
		{"", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.envValue+"_"+btoa(tt.def), func(t *testing.T) {
			key := "TEST_ENV_BOOL"
			if tt.envValue == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, tt.envValue)
			}
			defer os.Unsetenv(key)
			if got := envBool(key, tt.def); got != tt.want {
				t.Errorf("envBool(%q, %v) = %v, want %v", tt.envValue, tt.def, got, tt.want)
			}
		})
	}
}

func btoa(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestEnv(t *testing.T) {
	key := "TEST_ENV_STR"
	os.Setenv(key, "custom")
	defer os.Unsetenv(key)
	if got := env(key, "default"); got != "custom" {
		t.Errorf("env = %q, want custom", got)
	}
	os.Unsetenv(key)
	if got := env(key, "default"); got != "default" {
		t.Errorf("env = %q, want default", got)
	}
}
