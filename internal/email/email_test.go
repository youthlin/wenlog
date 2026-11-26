package email

import (
	"io"
	"mime"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestSMTPTimeoutIsShort(t *testing.T) {
	if smtpTimeout.Seconds() > 15 {
		t.Fatalf("smtpTimeout=%s, should stay short for admin test mail", smtpTimeout)
	}
}

func TestConfigTLSModeAndAuth(t *testing.T) {
	if !(Config{Port: 465}).implicitTLS() {
		t.Fatal("port 465 should use implicit TLS")
	}
	if (Config{Port: 587}).implicitTLS() {
		t.Fatal("port 587 should not use implicit TLS")
	}
	if (Config{}).auth() != nil {
		t.Fatal("empty SMTP user should not create auth")
	}
	if (Config{Host: "smtp.example.com", User: "user@example.com"}).auth() == nil {
		t.Fatal("SMTP user should create auth")
	}
}

func TestConfiguredRequiresSender(t *testing.T) {
	if (Config{Host: "smtp.example.com", Port: 587}).Configured() {
		t.Fatal("SMTP without sender should not be configured")
	}
	if !(Config{Host: "smtp.example.com", Port: 587, From: "noreply@example.com"}).Configured() {
		t.Fatal("SMTP with host, port and sender should be configured")
	}
}

func TestChineseSubjectIsMIMEEncoded(t *testing.T) {
	subject := "我的博客 - 数据库备份"
	for name, message := range map[string]string{
		"plain":      buildMessage("sender@example.com", "recipient@example.com", subject, "正文"),
		"attachment": buildMultipartMessage("sender@example.com", "recipient@example.com", subject, "正文", "backup.db", []byte("backup")),
	} {
		t.Run(name, func(t *testing.T) {
			parsed, err := mail.ReadMessage(strings.NewReader(message))
			if err != nil {
				t.Fatal(err)
			}
			header := parsed.Header.Get("Subject")
			for _, ch := range []byte(header) {
				if ch > 127 {
					t.Fatal("subject header contains raw non-ASCII bytes")
				}
			}
			decoded, err := new(mime.WordDecoder).DecodeHeader(header)
			if err != nil || decoded != subject {
				t.Fatalf("decoded subject=%q, err=%v", decoded, err)
			}
		})
	}
}

func TestSMTPGreetingHasDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		// 不发送欢迎消息，模拟连接成功但服务器不响应。
		conn.SetDeadline(time.Now().Add(smtpTimeout + 5*time.Second))
		_, err = io.Copy(io.Discard, conn)
		done <- err
	}()
	cfg := Config{Host: "127.0.0.1"}
	started := time.Now()
	client, conn, err := cfg.newClient(listener.Addr().String())
	if conn != nil {
		conn.Close()
	}
	if client != nil {
		client.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected greeting timeout, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > smtpTimeout+3*time.Second {
		t.Fatalf("greeting took %s", elapsed)
	}
	if err := <-done; err != nil {
		t.Fatalf("server connection did not close: %v", err)
	}
}
