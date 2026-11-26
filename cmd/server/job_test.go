package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/youthlin/wenlog/internal/consts"
)

func TestAutoBackupEmailAttachmentAndFailure(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	path, err := st.BackupDB()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 默认不发送，也不读取附件。
	if err := sendAutoBackupEmail(ctx, st, "missing.db"); err != nil {
		t.Fatal(err)
	}
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
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := textproto.NewReader(bufio.NewReader(conn))
		fmt.Fprint(conn, "220 localhost SMTP\r\n")
		for {
			line, err := reader.ReadLine()
			if err != nil {
				done <- err
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL FROM:"):
				fmt.Fprint(conn, "250 OK\r\n")
			case strings.HasPrefix(line, "RCPT TO:"):
				if line != "RCPT TO:<backup@example.com>" {
					done <- fmt.Errorf("unexpected recipient %q", line)
					return
				}
				fmt.Fprint(conn, "250 OK\r\n")
			case line == "DATA":
				fmt.Fprint(conn, "354 Send data\r\n")
				data, err := io.ReadAll(reader.DotReader())
				if err == nil {
					err = checkBackupMessage(data, path, original)
				}
				if err != nil {
					done <- err
					return
				}
				fmt.Fprint(conn, "250 Queued\r\n")
			case line == "QUIT":
				fmt.Fprint(conn, "221 Bye\r\n")
				done <- nil
				return
			default:
				done <- fmt.Errorf("unexpected SMTP command %q", line)
				return
			}
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	settings := map[string]string{
		consts.SettingsAutoBackupEmailEnabled: "true", consts.SettingsAutoBackupEmailTo: "backup@example.com",
		consts.SettingsSMTPHost: "127.0.0.1", consts.SettingsSMTPPort: strconv.Itoa(port),
		consts.SettingsSMTPFrom: "sender@example.com", consts.SettingsSiteName: "Test blog",
	}
	if err := st.UpdateSettings(ctx, settings, nil); err != nil {
		t.Fatal(err)
	}
	if err := sendAutoBackupEmail(ctx, st, path); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	listener.Close()
	// SMTP 拒绝连接时应报错，并且不删除已创建的备份。
	if err := sendAutoBackupEmail(ctx, st, path); err == nil {
		t.Fatal("expected SMTP connection failure")
	}
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(retained, original) {
		t.Fatalf("local backup changed: %v", err)
	}
}

func checkBackupMessage(data []byte, path string, original []byte) error {
	message, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return err
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil {
		return err
	}
	if !strings.Contains(subject, "Test blog") {
		return fmt.Errorf("missing site name")
	}
	_, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		return err
	}
	parts := multipart.NewReader(message.Body, params["boundary"])
	body, err := parts.NextPart()
	if err != nil {
		return err
	}
	io.Copy(io.Discard, body)
	attachment, err := parts.NextPart()
	if err != nil {
		return err
	}
	if attachment.FileName() != filepath.Base(path) {
		return fmt.Errorf("wrong attachment name")
	}
	encoded, err := io.ReadAll(attachment)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(encoded), "\n") {
		if len(strings.TrimSuffix(line, "\r")) > 76 {
			return fmt.Errorf("base64 attachment line exceeds 76 characters")
		}
	}
	decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(encoded)))
	if err != nil {
		return err
	}
	if !bytes.Equal(decoded, original) {
		return fmt.Errorf("attachment differs from database backup")
	}
	return nil
}
