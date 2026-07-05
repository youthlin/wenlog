package i18n

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"
)

func TestMiddlewareAndInject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gettext.SetGlobal(gettext.NewTranslations())
	if err := Init(); err != nil {
		t.Fatalf("init i18n: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/search?q=go&lang=en", nil)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	c.Request = req

	called := false
	Middleware()(c)
	called = true
	if !called {
		t.Fatal("middleware should continue request")
	}

	translator := gettext.WithContext(c.Request.Context())
	if got := translator.UsedLocale(); got != "en_US" {
		t.Fatalf("used locale = %q, want en_US", got)
	}
	if cookie := w.Header().Get("Set-Cookie"); !strings.Contains(cookie, "lang=en_US") {
		t.Fatalf("set-cookie = %q, want lang=en_US", cookie)
	}

	data := Inject(c, gin.H{})
	ts, ok := data["t"].(*gettext.Translations)
	if !ok {
		t.Fatal("expected injected translator in data[t]")
	}
	if got := ts.T("搜索"); got != "Search" {
		t.Fatalf("translated text = %q, want Search", got)
	}
	if got, _ := data["htmlLang"].(string); got != "en-US" {
		t.Fatalf("html lang = %q, want en-US", got)
	}
	if got, _ := data["usedLocale"].(string); got != "en_US" {
		t.Fatalf("used locale in data = %q, want en_US", got)
	}
	langURL, ok := data["langURL"].(map[string]string)
	if !ok {
		t.Fatal("expected injected langURL map in data")
	}
	if got := langURL["zh_CN"]; !strings.Contains(got, "lang=zh_CN") {
		t.Fatalf("zh switch url = %q, want lang=zh_CN", got)
	}
	if got := langURL["en_US"]; !strings.Contains(got, "lang=en_US") {
		t.Fatalf("en switch url = %q, want lang=en_US", got)
	}
}

func TestInjectUsesRefererForNonGETLangURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gettext.SetGlobal(gettext.NewTranslations())
	if err := Init(); err != nil {
		t.Fatalf("init i18n: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/admin/settings/profile", nil)
	req.Header.Set("Referer", "http://example.com/admin/settings")
	c.Request = req

	Middleware()(c)
	data := Inject(c, gin.H{})
	langURL, ok := data["langURL"].(map[string]string)
	if !ok {
		t.Fatal("expected injected langURL map in data")
	}
	if got := langURL["en_US"]; got != "/admin/settings?lang=en_US" {
		t.Fatalf("en switch url = %q, want /admin/settings?lang=en_US", got)
	}
}
