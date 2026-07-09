# Hook Reference

本文集中记录当前标准 action / filter 的触发点、参数、返回值语义和推荐脚本签名。实际 hook 名称以 `hook/consts.go` 的 `hook.Consts` 为准；插件和主题脚本应优先通过 `hook.Consts.ActionXxx` / `hook.Consts.FilterXxx` 引用常量。

## 脚本注册约定

插件和主题脚本入口：

```go
package plugin

import "hook"

func Register(api *hook.API) error {
    api.AddFilter(hook.Consts.FilterPostTitle, func(value string, post hook.PostView) string {
        return value
    })
    return api.RegistrationError()
}
```

推荐写具体签名，例如 `func(value string, post hook.PostView) string`。需要请求级能力、翻译、设置或输出辅助时，可写 `func(api *hook.API, value any, args ...any) any` 或 `func(api *hook.API, args ...any)`。

Filter 的第一个参数始终是当前值；后续参数是下表里的 payload。Action 的参数从下表里的 payload 开始。具体签名也可以显式把 `context.Context` 放在第一参，供宿主内部代码使用。

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

| Hook | 常量 | 触发点 | 当前值 | Payload | 返回值语义 |
|---|---|---|---|---|---|
| `post.title` | `hook.Consts.FilterPostTitle` | `post_title` / `post_title_text` | 标题纯文本 `string` | `hook.PostView` | 纯文本，宿主或模板上下文负责 HTML 转义 |
| `post.excerpt_html` | `hook.Consts.FilterPostExcerptHTML` | `post_excerpt` | 摘要 HTML `string` | `hook.PostView` | 可信 HTML |
| `post.content_html` | `hook.Consts.FilterPostContentHTML` | `post_content`；Feed 路径调用 `Renderer.FilterPostContent` | 正文 HTML `string` | `hook.PostView` | 可信 HTML |
| `post.footer_html` | `hook.Consts.FilterPostFooterHTML` | `post_content` 的 `post.content_html` 之后；Feed 路径同样应用 | 正文 HTML `string` | `hook.PostView` | 可信 HTML |
| `comment.content_html` | `hook.Consts.FilterCommentContentHTML` | `comment_content` / `list_comments` | 已转义评论 HTML `string` | `hook.CommentView` | 可信 HTML |
| `widget.render_html` | `hook.Consts.FilterWidgetRenderHTML` | 单个 widget 渲染完成后 | 组件 HTML `string` | `hook.WidgetRenderView` | 可信 HTML |
| `head.meta` | `hook.Consts.FilterHeadMeta` | `head_meta` 模板函数生成 OpenGraph / Twitter Card 后 | meta 标签 HTML `string` | `hook.HeadMetaView` | 可信 HTML |

`post.title` 是文本 filter，适合追加标题前缀、替换短标题等。不要返回 HTML 标签：

```go
api.AddFilter(hook.Consts.FilterPostTitle, func(value string, post hook.PostView) string {
    if post.PostType == "page" {
        return value
    }
    return value + " - Notes"
})
```

`*_html` filter 是 HTML 出口。插件应只返回自己能保证安全的 HTML；把文章、评论、用户输入等数据插入 HTML 前必须使用 `api.EscapeHTML`。

```go
api.AddFilter(hook.Consts.FilterPostFooterHTML, func(api *hook.API, value any, args ...any) any {
    return value.(string) + `<aside class="post-license">` + api.EscapeHTML(api.T("转载请注明出处")) + `</aside>`
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
