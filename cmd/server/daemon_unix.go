//go:build unix && !ios

package main

import (
	"bytes"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/youthlin/blog/internal/config"
)

var daemonChild = flag.Bool("daemon-child", false, "内部参数: 后台子进程模式")

const (
	start   = "start"
	stop    = "stop"
	restart = "restart"
	run     = "run"

	// daemonEnvToken 是启动子进程时注入的环境变量，用于在 /proc 中识别本服务进程。
	daemonEnvToken = "BLOG_DAEMON=1"
)

func runDaemon(cfg *config.Config) bool {
	switch runMode(flag.Args()) {
	case start:
		if err := startDaemon(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "后台启动失败, err=%v\n", err)
			os.Exit(1)
		}
		return true
	case stop:
		if err := stopDaemon(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "停止服务失败, err=%v\n", err)
			os.Exit(1)
		}
		return true
	case restart:
		if err := restartDaemon(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "重启服务失败, err=%v\n", err)
			os.Exit(1)
		}
		return true
	}
	return false
}

func runMode(args []string) string {
	if len(args) == 0 {
		return run
	}
	switch args[0] {
	case run, start, stop, restart:
		return args[0]
	default:
		return run
	}
}

func startDaemon(cfg *config.Config) error {
	if err := ensureRuntimeDir(cfg); err != nil {
		return err
	}
	pidFile := daemonPIDFile(cfg)
	if pid, running, err := readRunningPID(pidFile); err != nil {
		return err
	} else if running {
		return fmt.Errorf("服务已在后台运行, pid=%d, pidfile=%s", pid, pidFile)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logFile := daemonLogFile(cfg)
	logf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd := exec.Command(exe, "-daemon-child")
	cmd.Dir = mustGetwd()
	cmd.Env = append(os.Environ(), daemonEnvToken)
	cmd.Stdin = devNull
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if pid, running, err := readRunningPID(pidFile); err == nil && running {
			if err := checkHealth(cfg); err == nil {
				fmt.Printf("后台启动成功, 访问: %s, pidfile: %s(%d), log: %s\n",
					healthBaseURL(cfg), pidFile, pid, logFile)
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("子进程未在预期时间内就绪, 请查看日志: %s", logFile)
}

func ensureRuntimeDir(cfg *config.Config) error {
	return os.MkdirAll(filepath.Dir(daemonPIDFile(cfg)), 0o755)
}

func daemonPIDFile(cfg *config.Config) string {
	return filepath.Join(filepath.Dir(cfg.DBPath), "blog.pid")
}

func daemonLogFile(cfg *config.Config) string {
	return filepath.Join(filepath.Dir(cfg.DBPath), "blog.log")
}

func readRunningPID(pidFile string) (pid int, running bool, err error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		_ = os.Remove(pidFile)
		return 0, false, nil
	}
	if !processExists(pid) || !processLooksLikeSelf(pid) {
		_ = os.Remove(pidFile)
		return pid, false, nil
	}
	return pid, true, nil
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func checkHealth(cfg *config.Config) error {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get(healthBaseURL(cfg) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz status=%d", resp.StatusCode)
	}
	return nil
}

func healthBaseURL(cfg *config.Config) string {
	addr := cfg.Addr
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func writePidFile(cfg *config.Config) func() {
	if *daemonChild {
		if err := ensureRuntimeDir(cfg); err != nil {
			slog.Error("创建数据目录失败", slog.Any("error", err))
			os.Exit(1)
		}
		pidFile := daemonPIDFile(cfg)
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
			slog.Error("写入pid文件失败", slog.Any("error", err), slog.String("file", pidFile))
			os.Exit(1)
		}
		return func() {
			os.Remove(pidFile)
		}
	}
	return func() {}
}

func stopDaemon(cfg *config.Config) error {
	pidFile := daemonPIDFile(cfg)
	pid, running, err := readRunningPID(pidFile)
	if err != nil {
		return err
	}
	if !running {
		fmt.Printf("服务未运行, pidfile: %s\n", pidFile)
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			_ = os.Remove(pidFile)
			fmt.Printf("服务已停止, pid=%d\n", pid)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("已发送停止信号, 但进程仍未退出, pid=%d", pid)
}

func restartDaemon(cfg *config.Config) error {
	pidFile := daemonPIDFile(cfg)
	if pid, running, err := readRunningPID(pidFile); err != nil {
		return err
	} else if running {
		fmt.Printf("检测到后台服务运行中, 准备重启, pid=%d\n", pid)
		if err := stopDaemon(cfg); err != nil {
			return err
		}
	} else {
		fmt.Printf("服务当前未运行, 直接启动\n")
	}
	return startDaemon(cfg)
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func processLooksLikeSelf(pid int) bool {
	if pid <= 0 {
		return false
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(daemonEnvToken))
}
