package email

import "testing"

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
