# 模板数据字段参考

本文档列出 Go 代码传给前台模板的所有数据字段，供主题开发参考。

## 通用字段（所有页面）

`base()` 函数注入的字段，所有前台页面模板都能访问：

| 字段 | 类型 | 说明 |
|---|---|---|
| `SiteName` | `string` | 站点名称 |
| `Title` | `string` | 页面标题（`<title>` 标签） |
| `Description` | `string` | 页面描述（`<meta name="description">`） |
| `Menu` | `[]model.Post` | 导航菜单页面列表 |
| `CurrentYear` | `int` | 当前年份 |
| `CurrentUserID` | `uint` | 当前登录用户 ID，0 表示未登录 |
| `CurrentUser` | `*model.User` | 当前登录用户，未登录为 nil |
| `DefaultAvatar` | `string` | 默认头像类型（如 `cravatar`） |
| `CSRFToken` | `string` | CSRF 令牌 |
| `RegistrationOpen` | `bool` | 是否开放注册 |
| `MailEnabled` | `bool` | 邮件服务是否已配置 |
| `Widgets` | `[]interface{}` | widget 渲染结果（`template.HTML` 片段列表） |
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

| 函数 | 签名 | 说明 |
|---|---|---|
| `postURL` | `func(*model.Post) string` | 文章永久链接 |
| `pageURL` | `func(*model.Post) string` | 页面永久链接 |
| `categoryURL` | `func(string) string` | 分类归档链接 |
| `tagURL` | `func(string) string` | 标签归档链接 |
| `safeHTML` | `func(string) template.HTML` | 输出原始 HTML |
| `escapeHTML` | `func(string) string` | HTML 转义 |
| `listHTML` | `func(*model.Post) template.HTML` | 列表页正文（截断 more 标记前） |
| `detailHTML` | `func(*model.Post) template.HTML` | 详情页完整正文 |
| `hasMore` | `func(string) bool` | 是否有 more 标记 |
| `gravatar` | `func(string) string` | 头像 URL |
| `avatarURL` | `func(string, string) string` | 头像 URL（带默认头像） |
| `avatarPreviewURL` | `func(string) string` | 头像预览 URL |
| `gravatarPrimary` | `func(string) string` | Gravatar 主源 URL |
| `gravatarFallback` | `func(string) string` | Gravatar 回退 URL |
| `fmtDate` | `func(time.Time) string` | 格式化日期 `2006-01-02` |
| `fmtDateTime` | `func(time.Time) string` | 格式化日期时间 `2006-01-02 15:04` |
| `fmtFileSize` | `func(int64) string` | 格式化文件大小 |
| `year` | `func(time.Time) int` | 提取年份 |
| `add` | `func(int, int) int` | 加法 |
| `sub` | `func(int, int) int` | 减法 |
| `seq` | `func(int) []int` | 生成 `[1..n]` 整数切片 |

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

## 主题配置（theme.yaml）

```yaml
name: "主题名称"
version: "1.0"
description: "主题描述"
author: "作者"

pages:
  index:       # 首页
    data:      # 需要查询的数据字段
      - RecentPosts
      - RecentComments
      - SayingComments
      - ArchiveMonths
      - Categories
      - Tags
    widgets:   # widget 列表
      - user_info
      - search
      - saying
      - recent_posts
      - recent_comments
      - archive_months
      - categories
      - tags

  post:        # 文章详情
    data: [...]
    widgets: [...]

  page:        # 页面
    data: [...]
    widgets: [...]

  list:        # 列表页（搜索/分类/标签）
    data: [...]
    widgets: [...]

  archive:     # 归档页
    data: [...]
    widgets: [...]

  error:       # 错误页
    data: []
    widgets: []
```

### 可用 data 字段

| 字段 | 说明 | 对应模板变量 |
|---|---|---|
| `RecentPosts` | 近期文章 | `.RecentPosts` (`[]model.Post`) |
| `RecentComments` | 近期评论 | `.RecentCommentItems` (`[]store.CommentWidgetItem`) |
| `SayingComments` | 博主动态 | `.SayingCommentItems` + `.SayingPost` |
| `ArchiveMonths` | 归档月份 | `.ArchiveMonths` (`[]store.ArchiveMonth`) |
| `Categories` | 分类列表 | `.Categories` (`[]model.Category`) |
| `Tags` | 标签列表 | `.Tags` (`[]model.Tag`) |

### 可用 widget

| Widget 名 | 说明 |
|---|---|
| `user_info` | 用户信息（登录/欢迎） |
| `search` | 搜索框 |
| `saying` | 博主动态 |
| `recent_posts` | 近期文章 |
| `recent_comments` | 近期评论 |
| `archive_months` | 归档月份 |
| `categories` | 分类目录 |
| `tags` | 标签 |

## 模板继承结构

主题模板通过 Go `html/template` 的 `define`/`template` 机制工作。默认模板定义了以下命名模板：

| 模板名 | 说明 |
|---|---|
| `header` | 页面头部（`<head>` + `<header>` + `<main>` 开始） |
| `footer` | 页面尾部（`</main>` + widgets + 页脚 + `</body></html>`） |
| `widgets` | widget 区域（遍历 `.Widgets`） |
| `pagination` | 分页导航 |
| `comments` | 评论区域（含评论列表和评论表单） |
| `comments_fragment.gohtml` | 评论列表片段（AJAX 用） |
| `csrf_field` | CSRF 隐藏字段 |

每个页面模板（如 `index.gohtml`）通过 `{{template "header" .}}` / `{{template "footer" .}}` 包裹内容。

主题可以覆盖任意命名模板。主题未提供的模板自动回退到默认模板。

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

## Widget HTML 自定义

内置 widget 的数据由 Go 代码提供，默认 HTML 位于模板中的 `widget_{name}` 命名模板。主题可以定义同名模板覆盖 HTML 结构；widget 模板会注入 `.t.T` 等 i18n 字段，可以像普通页面模板一样翻译文案。

| Widget 名 | 覆盖模板名 | 数据类型 |
|---|---|---|
| `user_info` | `widget_user_info` | `theme.UserInfoWidgetData` |
| `search` | `widget_search` | `theme.SearchWidgetData` |
| `saying` | `widget_saying` | `theme.SayingWidgetData` |
| `recent_posts` | `widget_recent_posts` | `theme.RecentPostsWidgetData` |
| `recent_comments` | `widget_recent_comments` | `theme.RecentCommentsWidgetData` |
| `archive_months` | `widget_archive_months` | `theme.ArchiveMonthsWidgetData` |
| `categories` | `widget_categories` | `theme.CategoriesWidgetData` |
| `tags` | `widget_tags` | `theme.TagsWidgetData` |

示例：

```gohtml
{{define "widget_recent_posts"}}
<section class="widget widget-recent-posts">
  <h3>{{.t.T "最新文章"}}</h3>
  <ul>
    {{range .Posts}}<li><a href="{{postURL .}}">{{.Title}}</a></li>{{end}}
  </ul>
</section>
{{end}}
```
