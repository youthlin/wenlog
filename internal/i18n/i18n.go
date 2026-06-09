// Package i18n 负责初始化翻译文件、为每个请求选择语言,并向模板注入翻译能力。
package i18n

import (
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"

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

// 某些模板写法(条件/嵌套调用/属性内调用)目前无法被 xtemplate 稳定抽取,
// 这里用 gettext.Mark.T 显式保留消息,避免后台/前台零散文案漏翻。
var templateTranslationAnchors = []string{
	gettext.Mark.T("批准"),
	gettext.Mark.T("编辑"),
	gettext.Mark.T("驳回"),
	gettext.Mark.T("确认删除?"),
	gettext.Mark.T("暂无评论"),
	gettext.Mark.T("上一页"),
	gettext.Mark.T("下一页"),
	gettext.Mark.T("默认只读，仅允许 SELECT / EXPLAIN。该入口不在后台导航展示，请直接输入 URL 进入。"),
	gettext.Mark.T("Result Set(JSON)"),
	gettext.Mark.T("注意：如果数据库中已存在相同 ID 的文章、页面或评论，本次提交会按 upsert 覆盖保存。"),
	gettext.Mark.T("XML 文件"),
	gettext.Mark.T("导入归属用户"),
	gettext.Mark.T("请选择用户"),
	gettext.Mark.T("开始导入"),
	gettext.Mark.T("当前没有可选用户，请先创建管理员账号。"),
	gettext.Mark.T("最近一次导入结果"),
	gettext.Mark.T("文件：%s"),
	gettext.Mark.T("归属用户：%s"),
	gettext.Mark.T("说明：若只勾选“评论”而不勾选文章/页面，无法形成可回导 XML，因此提交时会提示错误。"),
	gettext.Mark.T("编辑%s"),
	gettext.Mark.T("新建%s"),
	gettext.Mark.T("Slug(页面链接 /slug)"),
	gettext.Mark.T("导航顺序(0 = 不在顶部导航显示,大于 0 为排序)"),
	gettext.Mark.T("还没有分类目录，可先去 分类/标签管理 新建分类。"),
	gettext.Mark.T("标签(逗号分隔)"),
	gettext.Mark.T("如:Go, 教程, 随笔"),
	gettext.Mark.T("%s 管理"),
	gettext.Mark.T("已发布"),
	gettext.Mark.T("暂无内容"),
	gettext.Mark.T("当前模板模式：hot（本地模板 + 手动重新解析）"),
	gettext.Mark.T("当前模板模式：static（embed 内置模板）"),
	gettext.Mark.T("检测到本地 `web/templates` 目录。修改模板文件后，点击下面按钮才会重新解析并生效。"),
	gettext.Mark.T("重新解析模板"),
	gettext.Mark.T("当前正在使用编译进程序的模板。点击下面按钮后会把模板释放到 `web/templates`，并让当前进程立即切换到 hot 模式。"),
	gettext.Mark.T("释放出模板文件夹"),
	gettext.Mark.T("切回后当前进程会立即使用内嵌模板资源，本地 `web/templates` 目录会保留在磁盘上。"),
	gettext.Mark.T("使用内嵌模板资源"),
	gettext.Mark.T("当前资源模式：hot（本地资源目录，修改后立即生效）"),
	gettext.Mark.T("当前资源模式：static（embed 内置资源）"),
	gettext.Mark.T("检测到本地 `web/assets` 目录。CSS / JS 等资源文件修改后会立即生效，无需手动重新加载。"),
	gettext.Mark.T("当前正在使用编译进程序的资源文件。点击下面按钮后会把资源释放到 `web/assets`，当前进程也会立即优先读取本地目录。"),
	gettext.Mark.T("释放资源文件夹"),
	gettext.Mark.T("切回后后续请求会自动使用内嵌资源文件，本地 `web/assets` 目录会保留在磁盘上。"),
	gettext.Mark.T("使用内嵌资源文件"),
	gettext.Mark.T("翻译资源设置"),
	gettext.Mark.T("当前翻译模式：hot（本地翻译目录）"),
	gettext.Mark.T("当前翻译模式：static（embed 内置翻译资源）"),
	gettext.Mark.T("检测到本地 `web/i18n` 目录。当前进程会优先读取本地翻译资源。"),
	gettext.Mark.T("切回后当前进程会立即使用内嵌翻译资源，本地 `web/i18n` 目录会保留在磁盘上。"),
	gettext.Mark.T("使用内嵌翻译资源"),
	gettext.Mark.T("当前正在使用编译进程序的翻译资源。点击下面按钮后会把翻译资源释放到 `web/i18n`，并让当前进程立即优先读取本地目录。"),
	gettext.Mark.T("释放翻译资源目录"),
	gettext.Mark.T("已配置自定义 Session Secret。修改后所有登录用户都需要重新登录。"),
	gettext.Mark.T("当前仍在使用默认或环境变量提供的 Session Secret。修改后所有登录用户都需要重新登录。"),
	gettext.Mark.T("编辑分类"),
	gettext.Mark.T("新建分类"),
	gettext.Mark.T("保存分类"),
	gettext.Mark.T("创建分类"),
	gettext.Mark.T("编辑标签"),
	gettext.Mark.T("新建标签"),
	gettext.Mark.T("保存标签"),
	gettext.Mark.T("创建标签"),
	gettext.Mark.T("确认删除该分类吗？"),
	gettext.Mark.T("暂无分类"),
	gettext.Mark.T("确认删除该标签吗？"),
	gettext.Mark.T("暂无标签"),
	gettext.Mark.T("确认删除该文件?"),
	gettext.Mark.T("暂无文件"),
	gettext.Mark.T("评论已关闭"),
	gettext.Mark.T("阅读全文 →"),
	gettext.Mark.T("没有找到匹配的文章。"),
	gettext.Mark.T("草稿预览"),
	gettext.Mark.T("回复"),
	gettext.Mark.T("以 <strong>%s</strong> 的身份发表评论，"),
	gettext.Mark.T("登出"),
	gettext.Mark.T("昵称 *"),
	gettext.Mark.T("邮箱 *(不公开)"),
	gettext.Mark.T("网站(可选)"),
	gettext.Mark.T("说点什么吧…… *"),
	gettext.Mark.T("有回复时邮件通知我"),
	gettext.Mark.T("提交评论"),
	gettext.Mark.T("评论提交后需经审核才会显示。"),
}

var hot atomic.Bool
var hotConfigured atomic.Bool

// Init 初始化全局翻译配置。
// 当前仓库源码中的默认文案是中文,因此把源代码语言设为 zh_CN,英文通过 po 文件提供。
func Init() error {
	if !hotConfigured.Load() {
		hot.Store(pathExists("web/i18n"))
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
	gettext.SetLocale("zh_CN")
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

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
