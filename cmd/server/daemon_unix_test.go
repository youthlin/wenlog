//go:build unix && !ios

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/youthlin/blog/internal/config"
)

func TestRunMode(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "default run", args: nil, want: "run"},
		{name: "explicit run", args: []string{"run"}, want: "run"},
		{name: "start", args: []string{"start"}, want: "start"},
		{name: "stop", args: []string{"stop"}, want: "stop"},
		{name: "restart", args: []string{"restart"}, want: "restart"},
		{name: "unknown falls back to run", args: []string{"status"}, want: "run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runMode(tt.args); got != tt.want {
				t.Fatalf("runMode(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestHealthBaseURL(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "bind all ipv4", addr: ":8888", want: "http://127.0.0.1:8888"},
		{name: "wildcard host", addr: "0.0.0.0:9999", want: "http://127.0.0.1:9999"},
		{name: "ipv6 wildcard", addr: "[::]:7777", want: "http://127.0.0.1:7777"},
		{name: "specific host", addr: "127.0.0.1:8080", want: "http://127.0.0.1:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Addr: tt.addr}
			if got := healthBaseURL(cfg); got != tt.want {
				t.Fatalf("healthBaseURL(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

func TestReadRunningPIDRemovesStaleKernelPID(t *testing.T) {
	if _, err := os.Stat("/proc/1/environ"); err != nil {
		t.Skip("requires /proc")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "blog.pid")
	if err := os.WriteFile(pidFile, []byte("1"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	_, running, err := readRunningPID(pidFile)
	if err != nil {
		t.Fatalf("readRunningPID: %v", err)
	}
	if running {
		t.Fatal("pid 1 should not be treated as this blog daemon")
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("stale pid file should be removed, stat err=%v", err)
	}
}
