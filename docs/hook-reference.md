# Hook Reference

本文集中记录当前标准 action / filter 的触发点、参数、返回值语义和推荐脚本签名。实际 hook 名称以 `hook/consts.go` 的 `hook.Consts` 为准；插件和主题脚本应优先通过 `hook.Consts.ActionXxx` / `hook.Consts.FilterXxx` 引用常量。

## 脚本注册约定

插件和主题脚本入口：

```go
package plugin

import "hook"

func Register(api *hook.API) error {
    api.AddFilter(hook.Consts.FilterPostTitle, func(api *hook.API, value string, post hook.PostView) string {
        return value
    })
    return api.RegistrationError()
}
```

**所有 action/filter 回调的第一个参数必须是 `*hook.API`**（请求级 api，含 ctx/DataLoader/ActionWriter），不再支持 `context.Context` 作为首参。核心 hook 使用下表中的具体类型签名；通用 hook 使用 `hook.ActionFunc` / `hook.FilterFunc` 签名。签名不匹配会在 `RegistrationError()` 中返回错误，插件加载失败。

- Action 标准签名：`func(api *hook.API, args ...any)`（即 `hook.ActionFunc`）
- 核心 Filter 标准签名见下表；通用 filter 可用 `func(api *hook.API, value any, args ...any) any`（即 `hook.FilterFunc`）
- RegisterFunc 签名：`func(api *hook.API, args hook.Args) any`
- Register 入口：`func Register(api *hook.API)` 或 `func Register(api *hook.API) error`

Filter 的第一个参数（api 之后）始终是当前值；后续参数是 payload。Action 的参数（api 之后）从 payload 开始。

## 执行隔离

Hook 处理器由 `hook.Registry` 串行执行，并带有基础隔离：

- 单个 action/filter handler 默认最多等待 1 秒。
- handler panic 会被 recover 并记录日志，不中断后续 handler。
- action handler 的输出先写入独立缓冲区；handler 按时完成后才 flush 到页面。超时时该 handler 的输出会被丢弃，避免半截 HTML 写入响应。
- filter handler 超时时保留进入该 handler 前的值，继续执行后续 filter。
- api 内部持有带超时的请求 ctx，可通过 `api.Debug/Info/Warn/Error` 打日志，无需手动获取 ctx。

限制：当前超时控制不能强制杀死已经进入死循环的 goroutine；它只保证调用方不继续等待，并让 action/filter 链按上面的降级语义继续执行。插件仍按管理员安装的可信代码处理，不是强沙箱。

## Actions

| Hook | 常量 | 触发点 | Payload | 用途 |
|---|---|---|---|---|
| `head.end` | `hook.Consts.ActionHeadEnd` | 主题模板 `{{do_action "head.end" .}}`，通常在 `</head>` 前 | 当前模板数据 `.` | 输出样式、脚本、meta 标签 |
| `body.end` | `hook.Consts.ActionBodyEnd` | 主题模板 `{{do_action "body.end" .}}`，通常在 `</body>` 前 | 当前模板数据 `.` | 输出延迟脚本、统计代码 |
| `comment.form.after_textarea` | `hook.Consts.ActionCommentFormAfterTextarea` | `comment_form` 模板函数输出评论 textarea 后 | 当前页面模板数据 | 输出表情面板、附件入口等评论表单控件 |
| `admin.post.form.after_textarea` | `hook.Consts.ActionAdminPostFormAfterTextarea` | 后台文章编辑表单 textarea 后 | 当前后台模板数据 | 输出编辑器增强控件 |

Action 通过 `api.Print` / `api.Printf` / `api.Println` 写 HTML。输出内容由插件负责转义用户数据；宿主不会再次转义 action 输出。

示例：

```go
api.AddAction(hook.Consts.ActionHeadEnd, func(api *hook.API, args ...any) {
    api.Println(`<link rel="stylesheet" href="/plugin-assets/demo/style.css">`)
})
```

## Filters

| Hook | 常量 | 触发点 | 当前值 | Payload | 返回值语义 | 标准签名 |
|---|---|---|---|---|---|---|
| `post.title` | `FilterPostTitle` | `post_title` / `post_title_text` | 标题纯文本 `string` | `hook.PostView` | 纯文本，宿主或模板上下文负责 HTML 转义 | `func(*API, string, PostView) string` |
| `post.excerpt_html` | `FilterPostExcerptHTML` | `post_excerpt` | 摘要 HTML `string` | `hook.PostView` | 可信 HTML | `func(*API, string, PostView) string` |
| `post.content_html` | `FilterPostContentHTML` | `post_content`；Feed 路径调用 `Renderer.FilterPostContent` | 正文 HTML `string` | `hook.PostView` | 可信 HTML | `func(*API, string, PostView) string` |
| `post.footer_html` | `FilterPostFooterHTML` | `post_content` 的 `post.content_html` 之后；Feed 路径同样应用 | 正文 HTML `string` | `hook.PostView` | 可信 HTML | `func(*API, string, PostView) string` |
| `comment.content_html` | `FilterCommentContentHTML` | `comment_content` / `list_comments` | 已转义评论 HTML `string` | `hook.CommentView` | 可信 HTML | `func(*API, string, CommentView) string` |
| `widget.render_html` | `FilterWidgetRenderHTML` | 单个 widget 渲染完成后 | 组件 HTML `string` | `hook.WidgetRenderView` | 可信 HTML | `func(*API, string, WidgetRenderView) string` |
| `head.meta` | `FilterHeadMeta` | `head_meta` 模板函数生成 OpenGraph / Twitter Card 后 | meta 标签 HTML `string` | `hook.HeadMetaView` | 可信 HTML | `func(*API, string, HeadMetaView) string` |
| `comment.before_create` | `FilterCommentBeforeCreate` | 评论入库前（CreateComment） | `*hook.CommentPreCreateView` | 无 | `*hook.CommentPreCreateView`，修改 Status 为 `spam` 拒绝、`pending` 待审核、`approved` 通过 | `func(*API, *CommentPreCreateView) *CommentPreCreateView` |

`post.title` 是文本 filter，适合追加标题前缀、替换短标题等。不要返回 HTML 标签：

```go
api.AddFilter(hook.Consts.FilterPostTitle, func(api *hook.API, value string, post hook.PostView) string {
    if post.PostType == "page" {
        return value
    }
    return value + " - Notes"
})
```

`*_html` filter 是 HTML 出口。插件应只返回自己能保证安全的 HTML；把文章、评论、用户输入等数据插入 HTML 前必须使用 `api.EscapeHTML`。

```go
api.AddFilter(hook.Consts.FilterPostFooterHTML, func(api *hook.API, value string, post hook.PostView) string {
    return value + `<aside class="post-license">` + api.EscapeHTML(api.T("转载请注明出处")) + `</aside>`
})
```

`comment.before_create` filter 用于评论提交前拦截（如反垃圾）：

```go
api.AddFilter(hook.Consts.FilterCommentBeforeCreate, func(api *hook.API, view *hook.CommentPreCreateView) *hook.CommentPreCreateView {
    if matched, _ := regexp.MatchString(`^[[:ascii:]]+$`, view.Content); matched {
        view.Status = "spam"
        view.RejectMessage = "评论内容不符合规则"
    }
    return view
})
```

## Payload Views

标准内容类 filter 传稳定只读 view，避免脚本依赖 GORM model：

| View | 用途 |
|---|---|
| `hook.PostView` | 文章/页面标题、摘要、正文、正文尾部 |
| `hook.CommentView` | 评论正文 |
| `hook.WidgetRenderView` | 组件最终 HTML 后处理 |
| `hook.HeadMetaView` | `<head>` meta 标签后处理 |
| `hook.CommentPreCreateView` | 评论提交前拦截（PostID/Author/Email/URL/IP/UserAgent/Content/Status/RejectMessage） |

View 字段见 `hook/view.go`。脚本里可直接访问 PascalCase 字段，例如 `post.Title`、`post.Author.DisplayName`、`comment.Author`、`widget.PluginID`。

## 模板作者注意

主题想让插件能力生效，应使用宿主模板函数，而不是直接输出底层字段：

| 场景 | 推荐写法 |
|---|---|
| 文章标题块 | `{{post_title .Post}}` |
| 列表、归档、上一篇/下一篇里的标题文本 | `{{post_title_text .}}` |
| 摘要 | `{{post_excerpt .}}` |
| 正文 | `{{post_content .Post}}` |
| 评论列表 | `{{list_comments .}}` |
| 评论表单 | `{{comment_form .}}` |
| 组件区域 | `{{render_widgets "sidebar" .}}` |
| head meta | `{{head_meta .}}` |

模板函数 `apply_filter` 返回可信 HTML，只适合明确的 HTML filter。文本类扩展应优先使用宿主封装函数。
