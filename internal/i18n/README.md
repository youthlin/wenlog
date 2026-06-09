# i18n 实现记录

本文档记录当前仓库国际化方案的关键约定，便于后续维护模板文案、PO 文件和抽取脚本。

## 1. 运行时方案

### 1.1 初始化与请求语言选择

- 入口：`cmd/server/main.go`
- 核心实现：`internal/i18n/i18n.go`

启动时会调用 `i18n.Init()`：

- 源码语言固定为 `zh_CN`
- 翻译文件目录为 `web/i18n`
- 优先加载本地目录；不存在时回退到 embed 文件系统

每个请求由 `i18n.Middleware()` 决定语言，优先级如下：

1. `?lang=`
2. Cookie `lang`
3. `Accept-Language`

命中的 locale 会写入 request context，后续通过 `gettext.WithContext(...)` 获取对应的 translator。

### 1.2 模板注入约定

模板渲染前统一调用 `i18n.Inject(c, data)`，向视图数据注入：

- `t`：`*gettext.Translations`
- `T` / `N` / `N64` / `X` / `XN` / `XN64`：translator 对应方法
- `usedLocale`：实际使用的 locale，例如 `en_US`
- `htmlLang`：适合放到 `<html lang>` 的值，例如 `en-US`
- `langURL`：语言切换链接 map，如 `langURL["en_US"]`

模板层统一使用以下写法：

```gohtml
<title>{{.t.T "hello, world"}}</title>
<html lang="{{.htmlLang}}">
<a href="{{index .langURL "en_US"}}">EN</a>
```

在 `range` / `with` 里如果当前 `.` 被切换，使用根数据：

```gohtml
{{range .List.Posts}}
  <a>{{$.t.T "编辑"}}</a>
{{end}}
```

之所以采用这种形式，是为了让 `xtemplate` 可以直接识别 `T/N/X...` 调用，而不需要再为模板额外维护一套 `tr(...)` 包装函数。

## 2. 文案维护约定

### 2.1 handler / Go 代码里的翻译

handler 或其他请求相关代码里统一先取请求级 translator：

```go
tr := i18n.Get(c)
tr.T("登录")
tr.N("%d 条评论", "%d 条评论", n, n)
tr.X("button", "关闭")
```

抽取脚本通过 `xgettext` 的 `T/N/N64/X/XN/XN64` 关键字直接抓取这类调用。

### 2.2 不能直接被 translator 调用抽取的文案

有些文案不会直接出现在 `tr.T(...)` / `tr.N(...)` 调用里，例如：

- 把翻译函数作为回调参数传递
- 需要先标记、后由其它函数格式化

这种场景统一使用：

```go
gettext.Mark.T("页面 slug 不能为空")
gettext.Mark.N("%d 条评论", "%d 条评论")
```

`gettext.Mark` 是库内置的 no-op 标记器，仅用于给 `xgettext` 提供抽取锚点。

### 2.3 带 HTML 的翻译字符串

如果翻译文案中确实需要保留少量 HTML（如 `<strong>`），必须确保用户输入先做 HTML 转义，再整体标记为安全 HTML。

当前示例：`web/templates/post.gohtml`

```gohtml
{{safeHTML (.t.T "以 <strong>%s</strong> 的身份发表评论，" (escapeHTML .CurrentUser.DisplayName))}}
```

不要把未经转义的用户输入直接插进 `safeHTML(...)`。

## 3. PO 文件维护

### 3.1 一键更新命令

仓库根目录执行：

```bash
./scripts/update_i18n.sh
```

脚本会完成：

1. 使用 `xgettext` 从 Go 源码抽取文案
2. 使用 `xtemplate` 从 `web/templates/*.gohtml` 抽取文案
3. 合并为 `web/i18n/messages.pot`
4. 对已有 `web/i18n/*.po` 运行 `msgmerge --update`

### 3.2 当前抽取规则

Go 源码抽取关键字：

- `T:1`
- `N:1,2`
- `N64:1,2`
- `X:1c,2`
- `XN:1c,2,3`
- `XN64:1c,2,3`

模板抽取关键字：

- `T`
- `N:1,2`
- `N64:1,2`
- `X:1c,2`
- `XN:1c,2,3`
- `XN64:1c,2,3`

对应脚本见：`scripts/update_i18n.sh`

### 3.3 翻译文件

- 模板 + Go 代码抽取结果：`web/i18n/messages.pot`
- 当前英文词库：`web/i18n/en_US.po`

新增语言时，复制 `.po` 文件并设置正确的 `Language:` 头即可。

## 4. 语言切换链接约定

`langURL` 默认基于当前页面生成，但在非 GET 请求（例如后台表单校验失败后直接回显模板）时，会优先使用 `Referer` 生成切换链接，避免语言切换入口落到 POST-only 路径导致 404/405。

如果某个页面有更特殊的跳转需求，可以在调用 `i18n.Inject` 后自行覆盖 `data["langURL"]`。

## 5. 改动后建议检查项

每次调整 i18n 相关代码后，至少执行：

```bash
go test ./...
./scripts/update_i18n.sh
msgfmt --check-format -o /dev/null web/i18n/en_US.po
```

这样可以同时验证：

- 运行逻辑没有坏
- 抽取链路可用
- 合并后的 PO 文件格式合法
