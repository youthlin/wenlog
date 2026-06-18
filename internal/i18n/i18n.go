// Package i18n 负责初始化翻译文件、为每个请求选择语言,并向模板注入翻译能力。
package i18n

import (
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"
	"golang.org/x/text/language"

	"github.com/youthlin/blog/internal/util"
	"github.com/youthlin/blog/web"
)

const (
	// QueryParam 是手动切换语言时使用的查询参数。
	QueryParam = "lang"
	// CookieName 是保存用户语言偏好的 cookie 名称。
	CookieName = "lang"
)

var hot atomic.Bool
var hotConfigured atomic.Bool

// Init 初始化全局翻译配置。
// 当前仓库源码中的默认文案是中文,因此把源代码语言设为 zh_CN,英文通过 po 文件提供。
func Init() error {
	if !hotConfigured.Load() {
		hot.Store(util.PathExists("web/i18n"))
		hotConfigured.Store(true)
	}
	return loadTranslations()
}

// Hot 返回当前翻译资源是否处于本地目录优先模式。
func Hot() bool { return hot.Load() }

// SetHot 设置当前翻译资源是否优先使用本地目录。
func SetHot(v bool) {
	hot.Store(v)
	hotConfigured.Store(true)
}

func loadTranslations() error {
	gettext.SetGlobal(gettext.NewTranslations())
	gettext.SetSourceCodeLocale("zh_CN")
	// 不强制固定成中文, 让空值走系统默认语言匹配。
	// 这样启动阶段插入的初始内容也能跟随服务器默认语言做翻译。
	gettext.SetLocale("")
	if hot.Load() {
		if _, err := os.Stat("web/i18n"); err == nil {
			gettext.Load("web/i18n")
			return nil
		}
		hot.Store(false)
	}
	langFS, err := fs.Sub(web.I18n, "i18n")
	if err != nil {
		return err
	}
	gettext.LoadFS(langFS)
	return nil
}

// Reload 重新加载翻译资源: 本地目录存在时优先使用本地目录,否则回退到 embed。
func Reload() error {
	return loadTranslations()
}

// BindDomain 从本地路径加载翻译文件并绑定到指定文本域。
// path 可以是包含 .po/.mo 的目录，也可以是单个翻译文件。
func BindDomain(domain, path string) error {
	domain = strings.TrimSpace(domain)
	path = strings.TrimSpace(path)
	if domain == "" || path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		matches, err := filepath.Glob(filepath.Join(path, "*.po"))
		if err != nil {
			return err
		}
		moMatches, err := filepath.Glob(filepath.Join(path, "*.mo"))
		if err != nil {
			return err
		}
		if len(matches)+len(moMatches) == 0 {
			return nil
		}
	}
	gettext.Bind(domain, path)
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
	data["N1"] = translator.N1
	data["N64"] = translator.N64
	data["N1_64"] = translator.N1_64
	data["X"] = translator.X
	data["XN"] = translator.XN
	data["XN1"] = translator.XN1
	data["XN64"] = translator.XN64
	data["XN1_64"] = translator.XN1_64
	data["usedLocale"] = used
	data["htmlLang"] = htmlLang(used)
	data["langURL"] = switchURLs
	return data
}

// InjectDomain 向模板数据注入指定文本域的翻译能力。
// 主题模板使用独立 domain 时，可继续在模板里写 .t.T / .t.N 等调用。
func InjectDomain(c *gin.Context, data gin.H, domain string) gin.H {
	data = Inject(c, data)
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return data
	}
	translator := Get(c).D(domain)
	data["t"] = translator
	data["T"] = translator.T
	data["N"] = translator.N
	data["N1"] = translator.N1
	data["N64"] = translator.N64
	data["N1_64"] = translator.N1_64
	data["X"] = translator.X
	data["XN"] = translator.XN
	data["XN1"] = translator.XN1
	data["XN64"] = translator.XN64
	data["XN1_64"] = translator.XN1_64
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
