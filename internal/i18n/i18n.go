// Package i18n 负责初始化翻译文件、为每个请求选择语言,并向模板注入翻译能力。
package i18n

import (
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"
	"golang.org/x/text/language"

	"github.com/youthlin/blog/web"
)

const (
	// QueryParam 是手动切换语言时使用的查询参数。
	QueryParam = "lang"
	// CookieName 是保存用户语言偏好的 cookie 名称。
	CookieName = "lang"
)

var widgetTranslationAnchors = []string{
	gettext.Mark.T("博主动态"),
	gettext.Mark.T("近期文章"),
	gettext.Mark.T("近期评论"),
	gettext.Mark.T("发表在"),
	gettext.Mark.T("归档"),
	gettext.Mark.T("选择月份…"),
	gettext.Mark.T("%d 年 %d 月(%d)"),
	gettext.Mark.T("分类目录"),
	gettext.Mark.T("选择分类…"),
	gettext.Mark.T("标签"),
	gettext.Mark.T("选择标签…"),
	gettext.Mark.T("未分类"),
}

// Init 初始化全局翻译配置。
// 当前仓库源码中的默认文案是中文,因此把源代码语言设为 zh_CN,英文通过 po 文件提供。
func Init() error {
	gettext.SetGlobal(gettext.NewTranslations())
	gettext.SetSourceCodeLocale("zh_CN")
	gettext.SetLocale("zh_CN")
	if _, err := os.Stat("web/i18n"); err == nil {
		gettext.Load("web/i18n")
		return nil
	}
	langFS, err := fs.Sub(web.I18n, "i18n")
	if err != nil {
		return err
	}
	gettext.LoadFS(langFS)
	return nil
}

// Middleware 为当前请求决定语言,优先级: query lang > cookie > Accept-Language。
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		locale := matchedLocale(requestedLocale(c))
		ctx := gettext.SetCtxLocale(c.Request.Context(), locale)
		c.Request = c.Request.WithContext(ctx)
		c.Set(CookieName, locale)
		if strings.TrimSpace(c.Query(QueryParam)) != "" {
			http.SetCookie(c.Writer, &http.Cookie{
				Name:     CookieName,
				Value:    locale,
				Path:     "/",
				MaxAge:   365 * 86400,
				HttpOnly: false,
				SameSite: http.SameSiteLaxMode,
			})
		}
		c.Next()
	}
}

// Inject 向模板数据里注入翻译能力与语言切换链接。
// 模板层统一使用 .t.T / .t.N 等写法, 以便 xtemplate 直接抽取翻译文本。
func Inject(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}
	translator := gettext.Global()
	used := gettext.UsedLocale()
	switchURLs := map[string]string{
		"zh_CN": "?" + QueryParam + "=" + url.QueryEscape("zh_CN"),
		"en_US": "?" + QueryParam + "=" + url.QueryEscape("en_US"),
	}
	if c != nil {
		translator = Get(c)
		used = translator.UsedLocale()
		switchURLs["zh_CN"] = switchURL(c, "zh_CN")
		switchURLs["en_US"] = switchURL(c, "en_US")
	}
	if used == "" {
		used = "zh_CN"
	}
	data["t"] = translator
	data["T"] = translator.T
	data["N"] = translator.N
	data["N64"] = translator.N64
	data["X"] = translator.X
	data["XN"] = translator.XN
	data["XN64"] = translator.XN64
	data["usedLocale"] = used
	data["htmlLang"] = htmlLang(used)
	data["langURL"] = switchURLs
	return data
}

// Get 按请求返回适配的翻译实例。
func Get(c *gin.Context) *gettext.Translations {
	if c == nil || c.Request == nil {
		return gettext.Global()
	}
	return gettext.WithContext(c.Request.Context())
}

func requestedLocale(c *gin.Context) string {
	if c == nil {
		return gettext.UsedLocale()
	}
	if lang := strings.TrimSpace(c.Query(QueryParam)); lang != "" {
		return lang
	}
	return gettext.GetUserLang(c.Request, gettext.WithCookieName(CookieName))
}

func matchedLocale(preferred string) string {
	locales := gettext.Locales()
	if len(locales) == 0 {
		return "zh_CN"
	}
	if strings.TrimSpace(preferred) == "" {
		return locales[0]
	}
	supported := make([]language.Tag, 0, len(locales))
	for _, locale := range locales {
		supported = append(supported, language.Make(strings.ReplaceAll(locale, "_", "-")))
	}
	matcher := language.NewMatcher(supported)
	_, index, _ := matcher.Match(language.Make(strings.ReplaceAll(preferred, "_", "-")))
	if index >= 0 && index < len(locales) {
		return locales[index]
	}
	return locales[0]
}

func switchURL(c *gin.Context, locale string) string {
	base := switchBaseURL(c)
	if base == nil {
		return "?" + QueryParam + "=" + url.QueryEscape(locale)
	}
	u := *base
	q := u.Query()
	q.Set(QueryParam, locale)
	u.RawQuery = q.Encode()
	return u.RequestURI()
}

func switchBaseURL(c *gin.Context) *url.URL {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return nil
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		u := *c.Request.URL
		return &u
	}
	referer := strings.TrimSpace(c.Request.Referer())
	if referer == "" {
		u := *c.Request.URL
		return &u
	}
	ref, err := url.Parse(referer)
	if err != nil {
		u := *c.Request.URL
		return &u
	}
	if ref.IsAbs() {
		if c.Request.Host != "" && !sameHost(ref.Host, c.Request.Host) {
			u := *c.Request.URL
			return &u
		}
	}
	if ref.Path == "" && ref.RawPath == "" {
		u := *c.Request.URL
		return &u
	}
	return &url.URL{Path: ref.Path, RawPath: ref.RawPath, RawQuery: ref.RawQuery, Fragment: ref.Fragment}
}

func sameHost(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func htmlLang(locale string) string {
	if locale == "" {
		return "zh-CN"
	}
	return strings.ReplaceAll(locale, "_", "-")
}
