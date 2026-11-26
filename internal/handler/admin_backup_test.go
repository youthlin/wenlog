package handler

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/store"
)

func TestSaveAutoBackupEmailSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	h := &Admin{st: st}
	r := gin.New()
	r.Use(sessions.Sessions("backup_test", cookie.NewStore([]byte("backup-test-key"))))
	r.SetHTMLTemplate(template.Must(template.New("admin_backup.gohtml").Parse(`{{.Error}}|{{.Notice}}|{{.AutoBackupEmailTo}}`)))
	r.POST("/admin/backup/settings", h.SaveBackupSettings)
	post := func(to string, enabled bool) string {
		t.Helper()
		form := url.Values{"auto_backup_enabled": {"on"}, "auto_backup_time": {"04:30"}, "auto_backup_keep": {"5"}, "auto_backup_email_to": {to}}
		if enabled {
			form.Set("auto_backup_email_enabled", "on")
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/backup/settings", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d", w.Code)
		}
		return w.Body.String()
	}
	for _, to := range []string{"", "invalid", "one@example.com,two@example.com", "one@example.com\r\nBcc: other@example.com"} {
		post(to, true)
		values, err := st.GetSettings(ctx, consts.SettingsAutoBackupTime, consts.SettingsAutoBackupEmailEnabled)
		if err != nil {
			t.Fatal(err)
		}
		if values[consts.SettingsAutoBackupTime] != "" || values[consts.SettingsAutoBackupEmailEnabled] != "" {
			t.Fatal("invalid input persisted settings")
		}
	}
	post("backup@example.com", true)
	values, _ := st.GetSettings(ctx, consts.SettingsAutoBackupTime)
	if values[consts.SettingsAutoBackupTime] != "" {
		t.Fatal("missing SMTP should reject save")
	}
	if err := st.UpdateSettings(ctx, map[string]string{consts.SettingsSMTPHost: "smtp.example.com", consts.SettingsSMTPPort: "587", consts.SettingsSMTPFrom: "sender@example.com"}, nil); err != nil {
		t.Fatal(err)
	}
	post(" backup@example.com ", true)
	values, err = st.GetSettings(ctx, consts.SettingsAutoBackupTime, consts.SettingsAutoBackupEmailEnabled, consts.SettingsAutoBackupEmailTo)
	if err != nil {
		t.Fatal(err)
	}
	if values[consts.SettingsAutoBackupTime] != "04:30" || values[consts.SettingsAutoBackupEmailEnabled] != "true" || values[consts.SettingsAutoBackupEmailTo] != "backup@example.com" {
		t.Fatalf("unexpected saved settings: %v", values)
	}
	if err := st.SetSetting(ctx, consts.SettingsSMTPHost, ""); err != nil {
		t.Fatal(err)
	}
	post("backup@example.com", false)
	values, _ = st.GetSettings(ctx, consts.SettingsAutoBackupEmailEnabled, consts.SettingsAutoBackupEmailTo)
	if values[consts.SettingsAutoBackupEmailEnabled] != "false" || values[consts.SettingsAutoBackupEmailTo] != "backup@example.com" {
		t.Fatal("cannot disable email or retain recipient without SMTP")
	}
}
