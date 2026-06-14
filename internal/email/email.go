// Package email 提供邮件发送能力。
package email

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// Config 是 SMTP 配置。
type Config struct {
	Host     string // smtp.example.com
	Port     int    // 587
	User     string // user@example.com
	Password string
	From     string // 发件人地址
}

// Configured 返回当前配置是否足以展示/启用邮件能力。
func (c Config) Configured() bool {
	return strings.TrimSpace(c.Host) != "" && c.Port > 0 && strings.TrimSpace(c.From) != ""
}

// Send 发送纯文本邮件。
func (c Config) Send(to, subject, body string) error {
	if !c.Configured() {
		return fmt.Errorf("SMTP 未配置")
	}

	msg := buildMessage(c.From, to, subject, body)
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)

	auth := smtp.PlainAuth("", c.User, c.Password, c.Host)

	// 先尝试 STARTTLS
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}
	defer client.Quit()

	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{ServerName: c.Host}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS 失败: %w", err)
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}

	if err := client.Mail(c.From); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("设置收件人失败: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("开始发送数据失败: %w", err)
	}
	_, err = w.Write([]byte(msg))
	if err != nil {
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("关闭邮件数据失败: %w", err)
	}

	return nil
}

func buildMessage(from, to, subject, body string) string {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return sb.String()
}
