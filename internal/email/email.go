// Package email 提供邮件发送能力。
package email

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"path/filepath"
	"strings"
	"time"
)

const smtpTimeout = 10 * time.Second

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
	client, conn, err := c.newClient(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer client.Quit()
	_ = conn.SetDeadline(time.Now().Add(smtpTimeout))

	if !c.implicitTLS() {
		tlsConfig := &tls.Config{ServerName: c.Host}
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS 失败: %w", err)
			}
		}
	}

	if auth := c.auth(); auth != nil {
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

// SendWithAttachment 发送带附件的邮件。
// filename 是附件显示名称，data 是附件内容。
func (c Config) SendWithAttachment(to, subject, body, filename string, data []byte) error {
	if !c.Configured() {
		return fmt.Errorf("SMTP 未配置")
	}

	msg := buildMultipartMessage(c.From, to, subject, body, filename, data)
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	client, conn, err := c.newClient(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer client.Quit()
	_ = conn.SetDeadline(time.Now().Add(smtpTimeout))

	if !c.implicitTLS() {
		tlsConfig := &tls.Config{ServerName: c.Host}
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS 失败: %w", err)
			}
		}
	}

	if auth := c.auth(); auth != nil {
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

func (c Config) newClient(addr string) (*smtp.Client, net.Conn, error) {
	var (
		conn net.Conn
		err  error
	)
	dialer := &net.Dialer{Timeout: smtpTimeout}
	if c.implicitTLS() {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: c.Host})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}
	return client, conn, nil
}

func (c Config) implicitTLS() bool {
	return c.Port == 465
}

func (c Config) auth() smtp.Auth {
	if strings.TrimSpace(c.User) == "" {
		return nil
	}
	return smtp.PlainAuth("", c.User, c.Password, c.Host)
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

// buildMultipartMessage 构建带附件的 MIME multipart/mixed 邮件。
func buildMultipartMessage(from, to, subject, body, filename string, data []byte) string {
	boundary := "boundary_" + randomBoundary()
	contentType := mime.TypeByExtension(filepath.Ext(filename))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n")
	sb.WriteString("\r\n")

	// 正文部分
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	sb.WriteString("\r\n")

	// 附件部分
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Type: " + contentType + "\r\n")
	sb.WriteString("Content-Disposition: attachment; filename=\"" + filename + "\"\r\n")
	sb.WriteString("Content-Transfer-Encoding: base64\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(base64.StdEncoding.EncodeToString(data))
	sb.WriteString("\r\n")

	sb.WriteString("--" + boundary + "--\r\n")
	return sb.String()
}

func randomBoundary() string {
	b := make([]byte, 16)
	// 使用时间戳作为简单随机源，对邮件边界来说足够
	for i := range b {
		b[i] = byte(time.Now().UnixNano()>>(i*4)) & 0x0f
		if b[i] < 10 {
			b[i] += '0'
		} else {
			b[i] += 'a' - 10
		}
	}
	return string(b)
}
