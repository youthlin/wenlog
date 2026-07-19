# 模板数据字段参考

本文档列出 Go 代码传给前台模板的所有数据字段，供主题开发参考。

## 通用字段（所有页面）

`base()` 函数注入的字段，所有前台页面模板都能访问：

| 字段 | 类型 | 说明 |
|---|---|---|
| `SiteName` | `string` | 站点名称 |
| `SiteLogo` | `string` | 站点 Logo URL（后台设置，可为空） |
| `Title` | `string` | 页面标题（`<title>` 标签） |
| `Description` | `string` | 页面描述（`<meta name="description">`） |
| `Menu` | `[]model.Post` | 导航菜单页面列表（兜底数据，当 `Menus` 中无对应位置配置时使用） |
| `Menus` | `map[string][]theme.MenuItem` | 按菜单位置分组的菜单项（如 `"primary"` → 主导航菜单），由后台菜单配置 + 页面列表解析生成 |
| `ThemeName` | `string` | 当前主题名称（如 `"default"`） |
| `CurrentYear` | `int` | 当前年份 |
| `CurrentUserID` | `uint` | 当前登录用户 ID，0 表示未登录 |
| `CurrentUser` | `*model.User` | 当前登录用户，未登录为 nil |
| `DefaultAvatar` | `string` | 默认头像类型（如 `cravatar`） |
| `CSRFToken` | `string` | CSRF 令牌 |
| `RegistrationOpen` | `bool` | 是否开放注册 |
| `MailEnabled` | `bool` | 邮件服务是否已配置 |
| `RecentPosts` | `[]model.Post` | 最近文章（当前代码注入 20 篇） |
| `Categories` | `[]model.Category` | 全部分类 |
| `Tags` | `[]model.Tag` | 全部标签 |
| `ArchiveMonths` | `[]store.ArchiveMonth` | 归档月份统计 |
| `RecentCommentItems` | `[]store.CommentWidgetItem` | 近期评论组件数据（当前代码注入 8 条） |
| `CurrentTheme` | `*theme.Theme` | 当前/预览主题对象；可读取 `Name`、`Version`、`Description`、`Author` 等主题元数据 |
| `ThemeVersion` | `string` | 当前主题版本，用于资源版本号等场景 |
| `Keyword` | `string` | 当前搜索关键词；非搜索页通常为空字符串 |
| `CurrentUserName` | `string` | 当前用户显示名；未登录为空字符串 |
| `SQLDetails` | `*store.LazySQLDetails` | SQL 调试详情（仅管理员且开启时） |

### i18n 字段

由 `i18n.Inject()` 注入：

默认前台模板使用应用默认 domain；启用外部主题时，主题模板会使用 `theme.yaml` 的 `name` 作为 domain 注入翻译器，因此主题模板仍写 `.t.T`，但翻译来自主题包的 `i18n/*.po` / `i18n/*.mo`。

| 字段 | 类型 | 说明 |
|---|---|---|
| `t` | `*gettext.Translator` | 翻译器实例 |
| `T` | `func(string) string` | 翻译函数 |
| `N` | `func(string, string, int, int) string` | 复数翻译 |
| `N1` | `func(string, string, int) string` | 单数/复数翻译（自动取 count） |
| `N64` | `func(string, string, int64, int64) string` | 复数翻译（int64） |
| `N1_64` | `func(string, string, int64) string` | 单数/复数翻译（int64，自动取 count） |
| `X` | `func(string, string) string` | 带上下文的翻译 |
| `XN` | `func(string, string, string, string, int, int) string` | 带上下文的复数翻译 |
| `XN1` | `func(string, string, string, string, int) string` | 带上下文的单数/复数翻译 |
| `XN64` | `func(string, string, string, string, int64, int64) string` | 带上下文的复数翻译（int64） |
| `XN1_64` | `func(string, string, string, string, int64) string` | 带上下文的单数/复数翻译（int64） |
| `htmlLang` | `string` | HTML lang 属性值 |
| `usedLocale` | `string` | 当前使用的 locale（`zh_CN` / `en_US`） |
| `langURL` | `map[string]string` | 语言切换 URL |

## 模板函数

所有模板中可用的函数：

会触发标准 action / filter 的函数，其 hook 名称、payload view、返回值安全语义和脚本签名见 `docs/hook-reference.md`。

| 函数 | 签名 | 说明 |
|---|---|---|
| `postURL` | `func(any) string` | 文章或页面永久链接；支持 `model.Post`、`*model.Post` 和 `theme.PostView` |
| `categoryURL` | `func(string) string` | 分类归档链接 |
| `tagURL` | `func(string) string` | 标签归档链接 |
| `safeHTML` | `func(string) template.HTML` | 输出原始 HTML |
| `escapeHTML` | `func(string) string` | HTML 转义 |
| `postExcerptHTML` | `func(*model.Post) template.HTML` | 文章摘要 HTML（截断 more 标记前） |
| `detailHTML` | `func(*model.Post) template.HTML` | 详情页完整正文 |
| `hasMore` | `func(string) bool` | 是否有 more 标记 |
| `avatarURL` | `func(string, string, int) string` | 头像 URL（参数：邮箱、默认头像类型、尺寸像素；size<=0 时使用默认 48px） |
| `defaultAvatarURL` | `func(string, int) string` | 根据默认头像类型生成兜底头像 URL（参数：默认头像类型、尺寸像素；size<=0 时使用默认 48px） |
| `fmtDate` | `func(time.Time) string` | 格式化日期 `2006-01-02` |
| `fmtDateTime` | `func(time.Time) string` | 格式化日期时间 `2006-01-02 15:04` |
| `fmtDateTimeLocal` | `func(time.Time) string` | 格式化为 HTML datetime-local 可用值 `2006-01-02T15:04` |
| `fmtFileSize` | `func(int64) string` | 格式化文件大小 |
| `year` | `func(time.Time) int` | 提取年份 |
| `add` | `func(int, int) int` | 加法 |
| `sub` | `func(int, int) int` | 减法 |
| `seq` | `func(int) []int` | 生成 `[1..n]` 整数切片 |
| `toInt` | `func(string) int` | 字符串转整数，失败返回 0 |
| `default` | `func(def, val any) any` | 当 val 为空值时返回 def |
| `hook_invoke` | `func(string, ...any) any` | 调用当前主题或插件 `functions.goyaegi` 里通过 `api.RegisterFunc(...)` 注册的扩展函数 |
| `theme_option` | `func(string) string` | 读取主题全局选项 |
| `widget_option` | `func(string) string` | 在 `widget_<id>` 模板内读取当前组件实例选项 |
| `render_widgets` | `func(string, any) template.HTML` | 渲染指定组件区域，如 `{{render_widgets "sidebar" .}}` |
| `render_menu` | `func(string, ...any) template.HTML` | 渲染指定位置的导航菜单，如 `{{render_menu "primary" .}}`；支持多级下拉子菜单，自动从 `.Menus` 或 `.Menu` 取数据 |
| `do_action` | `func(string, any) template.HTML` | 触发 action hook 并收集扩展输出 HTML，如 `{{do_action "head.end" .}}` |
| `apply_filter` | `func(any, string, ...any) template.HTML` | 对输入值应用 HTML filter 链，返回值会作为可信 HTML 输出；文本类扩展优先使用宿主封装函数，如 `post_title` |
| `post_title` | `func(any) template.HTML` | 输出文章标题并应用 `post.title` filter；filter 扩展参数传 `hook.PostView`；filter 返回值按纯文本转义 |
| `post_title_text` | `func(any) string` | 输出文章标题文本并应用 `post.title` filter；适合列表链接、归档列表、上一篇/下一篇等已有结构内的标题文本；模板会按上下文自动转义 |
| `post_excerpt` | `func(any) template.HTML` | 输出文章摘要并应用 `post.excerpt_html` filter；filter 扩展参数传 `hook.PostView`；filter 返回值按可信 HTML 输出 |
| `post_content` | `func(any) template.HTML` | 输出文章正文并应用 `post.content_html` 与 `post.footer_html` filter；filter 扩展参数传 `hook.PostView`；filter 返回值按可信 HTML 输出 |
| `post_tags` | `func(any) template.HTML` | 输出文章标签链接 |
| `post_navigation` | `func(any, ...string) template.HTML` | 输出上一篇/下一篇文章导航 |
| `body_class` | `func(any) string` | 生成 `<body>` CSS class |
| `post_class` | `func(any, ...string) string` | 生成文章容器 CSS class |
| `comment_class` | `func(any, ...string) string` | 生成评论项 CSS class |
| `comment_content` | `func(any) template.HTML` | 输出评论正文并应用 `comment.content_html` filter；filter 扩展参数传 `hook.CommentView`；filter 返回值按可信 HTML 输出 |
| `head_meta` | `func(any) template.HTML` | 输出 OpenGraph / Twitter Card meta 标签并应用 `head.meta` filter；filter 扩展参数传 `hook.HeadMetaView`；filter 返回值按可信 HTML 输出 |
| `list_comments` | `func(...any) template.HTML` | 渲染评论列表 |
| `comment_form` | `func(any) template.HTML` | 渲染评论表单，内部会触发 `comment.form.after_textarea` action |
| `comments_pagination` | `func(any, ...int) template.HTML` | 渲染评论分页导航，读取 `.CommentPager`；第二个可选参数为 `midSize`，控制当前页左右显示几个页码，默认 `2` |
| `posts_pagination` | `func(any, ...int) template.HTML` | 渲染文章列表分页导航，读取 `.Pager`；第二个可选参数为 `midSize`，控制当前页左右显示几个页码，默认 `2` |

## 数据模型

### model.Post（文章/页面）

```go
type Post struct {
    ID            uint        // 文章 ID
    Title         string      // 标题
    Slug          string      // URL slug
    Content       string      // 渲染后的正文 HTML
    ContentMD     string      // Markdown 原文
    Excerpt       string      // 摘要
    AuthorID      uint        // 作者 ID
    Status        string      // published / draft
    PostType      string      // post / page
    ContentFormat string      // html / markdown
    CommentStatus string      // open / closed
    Views         int64       // 浏览量
    MenuOrder     int         // 导航排序
    PublishedAt   time.Time   // 发布时间
    ModifiedAt    time.Time   // 修改时间
    CommentCount  int64       // 评论数（不入库，渲染期填充）
    Author        User        // 作者
    Categories    []Category  // 分类
    Tags          []Tag       // 标签
    Comments      []Comment   // 评论
}
```

### model.User（用户）

```go
type User struct {
    ID          uint      // 用户 ID
    Username    string    // 用户名
    DisplayName string    // 显示名称
    Email       string    // 邮箱
    Role        string    // admin / author / subscriber
}
```

### model.Category（分类）

```go
type Category struct {
    ID          uint   // 分类 ID
    Name        string // 分类名
    Slug        string // URL slug
    Description string // 描述
    ParentID    uint   // 父分类 ID
    PostCount   int64  // 文章数
}
```

### model.Tag（标签）

```go
type Tag struct {
    ID        uint   // 标签 ID
    Name      string // 标签名
    Slug      string // URL slug
    PostCount int64  // 文章数
}
```

### model.Comment（评论）

```go
type Comment struct {
    ID            uint      // 评论 ID
    PostID        uint      // 所属文章 ID
    ParentID      uint      // 父评论 ID（0=顶层）
    ReplyToID     uint      // 回复目标评论 ID
    UserID        *uint     // 登录用户 ID（匿名=nil）
    Author        string    // 评论者昵称
    Email         string    // 评论者邮箱
    URL           string    // 评论者网站
    IP            string    // IP 地址
    Content       string    // 评论内容
    Status        string    // approved / pending / spam / deleted
    NotifyOnReply bool      // 是否订阅回复通知
    CreatedAt     time.Time // 评论时间
    ReplyToAuthor string    // 回复 @某人（不入库）
    CommenterRole string    // 评论者角色标记（不入库）
}
```

## 各页面特有字段

### 首页（index）

| 字段 | 类型 | 说明 |
|---|---|---|
| `List` | `*store.ListPostsResult` | 文章列表 |
| `Pager` | `gin.H` | 分页信息 |

### 文章详情（post）

| 字段 | 类型 | 说明 |
|---|---|---|
| `Post` | `*model.Post` | 当前文章 |
| `IsDraft` | `bool` | 是否为草稿 |
| `Comments` | `[]model.Comment` | 评论列表 |
| `CommentPager` | `gin.H` | 评论分页 |
| `CommentCount` | `int64` | 评论总数 |
| `CommentOpen` | `bool` | 评论是否开放 |
| `RememberedCommenter` | `*CommenterInfo` | 记住的评论者信息 |
| `PrevPost` | `*model.Post` | 上一篇文章 |
| `NextPost` | `*model.Post` | 下一篇文章 |

### 页面（page）

| 字段 | 类型 | 说明 |
|---|---|---|
| `Post` | `*model.Post` | 当前页面 |
| `Comments` | `[]model.Comment` | 评论列表 |
| `CommentPager` | `gin.H` | 评论分页 |
| `CommentCount` | `int64` | 评论总数 |
| `CommentOpen` | `bool` | 评论是否开放 |
| `RememberedCommenter` | `*CommenterInfo` | 记住的评论者信息 |

### 列表页（list：搜索/分类/标签）

| 字段 | 类型 | 说明 |
|---|---|---|
| `Heading` | `string` | 列表标题 |
| `Keyword` | `string` | 搜索关键词（仅搜索页） |
| `List` | `*store.ListPostsResult` | 文章列表 |
| `Pager` | `gin.H` | 分页信息 |

### 归档页（archive）

| 字段 | 类型 | 说明 |
|---|---|---|
| `Post` | `*model.Post` | 归档页面 |
| `Groups` | `[]struct{Year int; Posts []model.Post}` | 按年份分组的文章 |

### 错误页（error）

| 字段 | 类型 | 说明 |
|---|---|---|
| `Code` | `int` | HTTP 状态码 |
| `Message` | `string` | 错误消息 |

## 辅助类型

### store.ListPostsResult

```go
type ListPostsResult struct {
    Posts []model.Post
    Total int64
    Page  int
    Pages int
}
```

### Pager（gin.H）

```go
gin.H{
    "Page":    int,     // 当前页码
    "Pages":   int,     // 总页数
    "BaseURL": string,  // 分页基础 URL
    "Sep":     string,  // 分隔符 "?" 或 "&"
}
```

### CommentPager（gin.H）

```go
gin.H{
    "Page":    int,     // 当前评论页码
    "Pages":   int,     // 评论总页数
    "BaseURL": string,  // 评论分页基础 URL
    "Sep":     string,  // 分隔符
}
```

### CommenterInfo（记住的评论者）

```go
type CommenterInfo struct {
    Author string
    Email  string
    URL    string
}
```

### theme.MenuItem（菜单项）

`renderMenu` 渲染的菜单项结构，由后台菜单配置解析生成：

```go
type MenuItem struct {
    ID       string     // 唯一标识
    Title    string     // 菜单标题
    URL      string     // 链接地址
    Target   string     // 链接目标（_blank 等）
    Children []MenuItem // 子菜单项
}
```

## 主题配置（theme.yaml）

当前主题配置以 v6 格式为准：`theme.yaml` 包含主题元数据、组件区域、可用组件、组件级选项、菜单位置与主题全局选项。早期 `pages.data/widgets` 配置已经废弃，前台通用数据由 Go 代码统一注入，页面模板由文件名和模板层级决定。

```yaml
name: "my-theme"
version: "1.0"
description: "主题描述"
author: "作者"
author_uri: "https://example.com"
theme_uri: "https://example.com/theme"
license: "MIT"
tags: ["响应式", "双栏"]

widget_areas:
  sidebar:
    name: "侧边栏"
    description: "文章页侧边栏区域"

menu_locations:
  primary:
    name: "主导航"
    description: "站点顶部导航菜单"
  footer:
    name: "页脚导航"
    description: "站点页脚链接"

widgets:
  - id: recent_posts
    label: "近期文章"
    area: sidebar
    options:
      - id: count
        type: number
        label: "显示数量"
        default: "5"
        min: 1
        max: 20

  - id: custom_html
    label: "自定义 HTML"
    area: sidebar
    options:
      - id: html
        type: textarea
        label: "HTML 内容"
        default: ""

options:
  - id: custom_css
    type: css
    label: "自定义 CSS"
    description: "会注入到页面 <head> 中"
    default: ""
```

### 主题目录结构

```text
themes/my-theme/
├── theme.yaml
├── functions.goyaegi       # 可选：注册主题扩展函数和 hook
├── templates/
│   ├── index.gohtml        # 必需：首页和兜底模板
│   ├── post.gohtml         # 可选
│   ├── page.gohtml         # 可选
│   ├── list.gohtml         # 可选
│   ├── archive.gohtml      # 可选
│   ├── search.gohtml       # 可选
│   ├── error.gohtml        # 可选
│   └── fragment_comments.gohtml # 可选：评论局部渲染
├── widgets/
│   └── recent_posts.gohtml # 可选：覆盖/新增 widget_recent_posts 模板
├── assets/
│   └── style.css           # 通过 /theme-assets/style.css 访问
└── i18n/
    └── en_US.po
```

### 可用 data 字段（无需在 theme.yaml 声明）

| 字段 | 说明 | 对应模板变量 |
|---|---|---|
| `RecentPosts` | 近期文章 | `.RecentPosts` (`[]model.Post`) |
| `RecentCommentItems` | 近期评论 | `.RecentCommentItems` (`[]store.CommentWidgetItem`) |
| `ArchiveMonths` | 归档月份 | `.ArchiveMonths` (`[]store.ArchiveMonth`) |
| `Categories` | 分类列表 | `.Categories` (`[]model.Category`) |
| `Tags` | 标签列表 | `.Tags` (`[]model.Tag`) |
| `Menu` | 导航页面（兜底） | `.Menu` (`[]model.Post`) |
| `Menus` | 按位置分组的菜单 | `.Menus` (`map[string][]theme.MenuItem`) |

### 内置 widget

| Widget ID | 默认模板名 | 说明 |
|---|---|---|
| `search` | `widget_search` | 搜索框 |
| `recent_posts` | `widget_recent_posts` | 近期文章 |
| `categories` | `widget_categories` | 分类目录 |
| `tag_cloud` | `widget_tag_cloud` | 标签云 |
| `recent_comments` | `widget_recent_comments` | 近期评论 |
| `custom_html` | `widget_custom_html` | 自定义 HTML |
| `user_info` | `widget_user_info` | 用户信息 |

## 模板继承结构

主题模板通过 Go `html/template` 的 `define`/`template` 机制工作。前台页面按页面类型走模板层级 fallback：

| 页面类型 | 模板查找顺序 |
|---|---|
| 首页 | `index.gohtml` |
| 文章页 | `post.gohtml` → `index.gohtml` |
| 页面 | `page.gohtml` → `post.gohtml` → `index.gohtml` |
| 搜索 | `search.gohtml` → `list.gohtml` → `index.gohtml` |
| 分类/标签列表 | `list.gohtml` → `index.gohtml` |
| 归档 | `archive.gohtml` → `list.gohtml` → `index.gohtml` |
| 错误页 | `error.gohtml` |

主题可按需定义以下命名模板：

| 模板名 | 说明 |
|---|---|
| `header` | 页面头部（`<head>` + `<header>` + `<main>` 开始） |
| `footer` | 页面尾部（`</main>` + 组件区域 + 页脚 + `</body></html>`） |
| `pagination` | 分页导航 |
| `comments` | 评论区域（含评论列表和评论表单） |
| `fragment_<name>.gohtml` | 局部渲染片段（通过 `?fragment=<name>` 请求，如 `fragment_comments.gohtml`） |
| `csrf_field` | CSRF 隐藏字段 |

每个页面模板（如 `index.gohtml`）通过 `{{template "header" .}}` / `{{template "footer" .}}` 包裹内容。

主题应至少提供 `templates/index.gohtml`。前台主题之间不会互相 fallback；后台/认证基础模板会由渲染器补齐。

## 主题翻译文件

主题包可以提供自己的翻译文件：

```text
themes/my-theme/
├── theme.yaml        # name: "my-theme"
├── templates/
└── i18n/
    └── en_US.po
```

加载规则：

- domain 使用 `theme.yaml` 的 `name` 值，例如 `my-theme`。
- 主题激活后，页面模板和 widget 模板中的 `.t` 会注入为 `i18n.Get(c).D("my-theme")`。
- 模板里仍然使用 `.t.T` / `.t.N` / `.t.X` / `.t.XN` 等；不需要在模板中显式写 domain。
- 主题包上传时允许包含 `.po` / `.mo` 文件。
- 默认主题 `default` 使用应用默认翻译资源。

## 组件 HTML 自定义

内置组件模板来自 `web/widgets/*.gohtml`，主题可以在 `widgets/{id}.gohtml` 中定义同名 `widget_{id}` 模板覆盖 HTML 结构。组件模板和普通页面模板共用同一份数据上下文，因此可以直接访问 `.RecentPosts`、`.Categories`、`.Tags`、`.RecentCommentItems`、`.t.T` 等字段/函数。

| Widget ID | 覆盖模板名 | 典型数据来源 |
|---|---|---|
| `user_info` | `widget_user_info` | `.CurrentUser` / `.CurrentUserName` |
| `search` | `widget_search` | `.Keyword` |
| `recent_posts` | `widget_recent_posts` | `.RecentPosts` |
| `recent_comments` | `widget_recent_comments` | `.RecentCommentItems` |
| `categories` | `widget_categories` | `.Categories` |
| `tag_cloud` | `widget_tag_cloud` | `.Tags` |
| `custom_html` | `widget_custom_html` | `widget_option "html"` |

示例：

```gohtml
{{define "widget_recent_posts"}}
{{$n := widget_option "count" | toInt | default 5}}
<section class="widget widget-recent-posts">
  <h3>{{.t.T "最新文章"}}</h3>
  <ul>
    {{range $i, $p := .RecentPosts}}{{if lt $i $n}}
      <li><a href="{{postURL $p}}">{{post_title_text $p}}</a></li>
    {{end}}{{end}}
  </ul>
</section>
{{end}}
```

页面模板中渲染区域：

```gohtml
<aside class="sidebar">
  {{render_widgets "sidebar" .}}
</aside>
```

## 主题自定义数据（functions.goyaegi）

当通用模板数据不够用时，主题可提供 `functions.goyaegi` 或 `functions.go`，定义 `package theme` 与 `Register(api *hook.API)` 函数，通过 `api.RegisterFunc` 注册可由模板调用的扩展函数。`Register` 也可以返回 `error`，用于在加载阶段显式暴露初始化失败：

```go
package theme

import (
    "sort"

    "hook"
)

func Register(api *hook.API) error {
    api.RegisterFunc("PopularPosts", func(api *hook.API, args hook.Args) any {
        posts := api.Posts()
        sort.Slice(posts, func(i, j int) bool { return posts[i].Views > posts[j].Views })
        n := args.PositiveInt("n", 5)
        if len(posts) > n {
            posts = posts[:n]
        }
        return posts
    })
    return api.RegistrationError()
}
```

模板中调用：

```gohtml
{{range hook_invoke "PopularPosts" "n" 5}}
  <a href="{{postURL .}}">{{post_title_text .}}</a>
{{end}}
```
