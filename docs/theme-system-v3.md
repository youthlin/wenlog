# 主题系统 v3 设计方案

> 历史设计稿：当前实现以 `AGENTS.md`、`docs/template-data-reference.md` 和 `docs/theme-system-optimization-plan.md` 为准。

## 1. 动机：v2 的痛点

v2 开发一个主题需要同时维护三个地方：

| 步骤 | 文件 | 作用 |
|------|------|------|
| 1 | `functions.goyaegi` | 注册主题函数（自定义数据/变换逻辑） |
| 2 | `theme.yaml` | 声明 `widgets:` 列表（排序） |
| 3 | `base.gohtml` | 定义 `{{define "widget_xxx"}}` 模板 |

其中第 2 步和第 3 步是重复声明——模板已经叫 `widget_popular_posts` 了，yaml 里再写一遍 `popular_posts` 只是告诉系统渲染顺序。

**核心问题**：引入了"Widget"这个自定义概念，主题开发者需要学习主题函数、widget 注册、yaml 声明三套机制。

## 2. 借鉴 WordPress 的哲学

WordPress 主题开发的核心原则：

- **文件名即约定** — `single.php`、`page.php`、`archive.php`，不需要配置文件声明
- **模板标签全局可用** — `the_title()`、`the_content()`、`wp_list_pages()` 在任何模板里都能用
- **没有 Widget 抽象** — `sidebar.php` 就是个普通模板，直接写 HTML + 模板标签
- **模板层级自动 fallback** — `single.php` 不存在就用 `index.php`
- **最小主题只需两个文件** — `style.css` + `index.php`

## 3. v3 设计目标

1. **去掉 Widget 概念** — 侧边栏就是普通模板，直接写 HTML
2. **常用数据全局可用** — 模板里直接用 `.RecentPosts`、`.Categories`、`.Tags` 等
3. **模板文件名决定页面** — `index.gohtml`、`post.gohtml`、`page.gohtml`，自动 fallback
4. **`theme.yaml` 退化为纯元数据** — 只有 name、version、author、description
5. **`functions.goyaegi` 只写真正自定义的逻辑** — 简单查询不需要它，同时作为 `themeInvoke` 用法演示
6. **最小主题只需两个文件** — `theme.yaml` + `templates/index.gohtml`

## 4. 主题目录结构

```
themes/my-theme/
├── theme.yaml              # 仅元数据（name, version, author, description）
├── functions.goyaegi       # 可选，只写自定义数据变换（如 popular_posts）
├── templates/
│   ├── index.gohtml        # 首页 + 兜底模板（必须）
│   ├── post.gohtml         # 文章页（可选 → fallback index）
│   ├── page.gohtml         # 页面（可选 → fallback post → index）
│   ├── list.gohtml         # 分类/标签列表（可选 → fallback index）
│   ├── archive.gohtml      # 归档页（可选 → fallback list → index）
│   ├── search.gohtml       # 搜索结果（可选 → fallback list → index）
│   ├── error.gohtml        # 404/500（可选 → fallback index）
│   ├── fragment_comments.gohtml  # AJAX 评论片段（可选，不定义则无 AJAX）
│   ├── header.gohtml       # {{define "header"}}（可选，可内联在 index.gohtml）
│   ├── footer.gohtml       # {{define "footer"}}（可选，可内联在 index.gohtml）
│   └── sidebar.gohtml      # {{define "sidebar"}} — 就是个普通模板！
├── assets/                 # CSS/JS/图片
└── i18n/                   # 翻译文件 (.po/.mo)
```

> **最小主题**：只需 `theme.yaml` + `templates/index.gohtml` 两个文件。`index.gohtml` 中内联 header/footer/pagination/comments 等所有 define，并用数据特征（`.Code` / `.Groups` / `.Post` / `.List`）区分页面类型。参见 `themes/single/`。

## 5. 模板层级（Template Hierarchy）

仿 WordPress 的 fallback 链：

```
请求类型          查找顺序
─────────────────────────────────────────
首页              index.gohtml
文章页            post.gohtml → index.gohtml
页面              page.gohtml → post.gohtml → index.gohtml
分类列表          list.gohtml → index.gohtml
标签列表          list.gohtml → index.gohtml
归档列表          archive.gohtml → list.gohtml → index.gohtml
搜索结果          search.gohtml → list.gohtml → index.gohtml
404               error.gohtml
```

> **实现状态**：已实现。`render.Renderer.ResolveTemplate(pageType)` 按 `TemplateHierarchy` 查找主题中第一个存在的模板。跨主题 fallback 已移除——每个主题必须自包含。

## 6. 全局模板数据

所有模板中以下数据始终可用，无需在 yaml 中声明：

```go
// 站点信息
.SiteName          string
.SiteDescription   string
.SiteLogo          string  // 站点 Logo URL（后台设置，可为空）
.CurrentYear       int

// 当前用户
.CurrentUserID     uint
.CurrentUserName   string
.CSRFToken         string

// 文章数据（按发布时间倒序）
.RecentPosts       []model.Post  // 最近 8 篇

// 分类 & 标签
.Categories        []model.Category
.Tags              []model.Tag

// 评论
.RecentCommentItems []CommentWidgetItem  // 最近 8 条评论（含文章信息）
.SayingCommentItems []CommentWidgetItem  // 博主动态（saying 页面下博主评论）

// 归档
.ArchiveMonths     []ArchiveMonth

// 导航菜单
.Menu              []model.Post  // menu_order > 0 的页面

// 页面特定数据（仅在对应页面类型时存在）
.Post              *model.Post   // 当前文章/页面
.Comments          []model.Comment  // 当前文章评论
.Pager             *Pager         // 分页信息
.Keyword           string         // 搜索关键词

// 工具
.t                 *i18n.Translator
.langURL           map[string]string
.usedLocale        string
.htmlLang          string
.DefaultAvatar     string
```

> **注意**：`.PopularPosts` 不是全局数据，而是通过 `functions.goyaegi` 注册的主题函数提供，作为自定义主题逻辑的用法演示。模板中通过 `{{themeInvoke "popular_posts" "n" 5}}` 调用。

## 7. 模板约定

### 7.1 必须定义的模板块

```gohtml
{{define "header"}}...{{end}}   <!-- <!DOCTYPE html> 到 <main> 之前 -->
{{define "footer"}}...{{end}}   <!-- </main> 之后到 </html> -->
```

### 7.2 可选模板块

```gohtml
{{define "sidebar"}}...{{end}}  <!-- 侧边栏，在 footer 中通过 {{template "sidebar" .}} 调用 -->
{{define "head"}}...{{end}}     <!-- 额外的 <head> 内容（meta、link、script） -->
{{define "pagination"}}...{{end}}<!-- 分页导航 -->
```

### 7.3 完整示例：最小主题

**theme.yaml**:
```yaml
name: "minimal"
version: "1.0"
description: "极简主题"
author: "youthlin"
```

**templates/index.gohtml**:
```gohtml
{{define "header"}}<!DOCTYPE html>
<html lang="{{.htmlLang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="/theme-assets/style.css">
</head>
<body>
<header>
  <h1><a href="/">{{.SiteName}}</a></h1>
  <nav>{{range .Menu}}<a href="{{postURL .}}">{{.Title}}</a>{{end}}</nav>
</header>
<main>
{{end}}

{{define "footer"}}
</main>
<footer>&copy; {{.CurrentYear}} {{.SiteName}}</footer>
</body></html>
{{end}}

{{define "sidebar"}}
<aside>
  <section>
    <h3>搜索</h3>
    <form action="/search"><input name="q" value="{{.Keyword}}"></form>
  </section>
  <section>
    <h3>近期文章</h3>
    <ul>{{range .RecentPosts}}<li><a href="{{postURL .}}">{{.Title}}</a></li>{{end}}</ul>
  </section>
  <section>
    <h3>最热文章</h3>
    <ul>{{range themeInvoke "popular_posts" "n" 5}}<li><a href="{{postURL .}}">{{.Title}}</a></li>{{end}}</ul>
  </section>
  <section>
    <h3>分类</h3>
    <ul>{{range .Categories}}<li><a href="{{categoryURL .Slug}}">{{.Name}}</a></li>{{end}}</ul>
  </section>
</aside>
{{end}}
```

注意：侧边栏就是普通模板，直接写 HTML + 模板标签，不需要注册主题函数、不需要 yaml 声明、不需要 widget 概念。

## 8. functions.goyaegi 的角色变化

v2 中 `functions.goyaegi` 承担了大量"内置数据提供者"的注册（recent_posts、categories、tags 等），这些在 v3 中由系统直接注入模板数据，不再需要脚本注册。

v3 中 `functions.goyaegi` **只用于真正自定义的数据变换**。默认主题保留 `popular_posts` 主题函数作为 `themeInvoke` 用法演示：

```go
package theme

import (
    "sort"
    "themeapi"
)

// Register 注册主题的自定义主题函数。
// 常用数据（RecentPosts、Categories、Tags 等）已全局可用，无需在此注册。
// 这里演示如何通过 themeapi.Api 实现自定义主题逻辑。
// themeapi.Api 来自宿主真实的 themeapi 包，并由宿主注入为全局变量。
func Register() {
    // popular_posts: 热门文章（按浏览量排序）—— 演示自定义主题函数用法
    themeapi.Api.RegisterFunc("popular_posts", func(args map[string]any) any {
        n := getInt(args, "n", 5)
        posts := themeapi.Api.Posts()
        sort.Slice(posts, func(i, j int) bool {
            return posts[i].Views > posts[j].Views
        })
        if n > len(posts) {
            n = len(posts)
        }
        return posts[:n]
    })
}

func getInt(args map[string]any, key string, def int) int {
    if v, ok := args[key]; ok {
        switch n := v.(type) {
        case int:
            return n
        case int64:
            return int(n)
        case float64:
            return int(n)
        }
    }
    return def
}
```

模板中通过 `themeInvoke` 函数调用：

```gohtml
{{$popular := themeInvoke "popular_posts" "n" 5}}
{{range $popular}}<li><a href="{{postURL .}}">{{.Title}}</a></li>{{end}}
```

## 9. 与 v2 的对比

| 方面 | v2 | v3 |
|------|----|----|
| 最小主题文件数 | 3（theme.yaml + base.gohtml + functions.goyaegi） | 2（theme.yaml + index.gohtml） |
| 加一个侧边栏模块 | 改 3 个文件 | 在 sidebar.gohtml 写几行 HTML |
| 需要学习的概念 | Widget、主题函数、yaml widgets 列表 | 模板标签（和 WordPress 一样） |
| 数据声明 | yaml 里声明 `data:` 和 `widgets:` | 不需要，全局可用 |
| 模板文件名 | `base.gohtml`（所有页面共用） | `index.gohtml`、`post.gohtml` 等（按页面类型） |
| 模板 fallback | 无 | 自动 fallback 链 |
| functions.goyaegi | 必须注册所有数据源 | 只写自定义逻辑 |

## 10. 实现记录

### 10.1 已完成

| 文件 | 改动 |
|------|------|
| `internal/theme/theme.go` | `PageConfig` 结构体已完全移除；`Theme` 结构体仅保留元数据字段 |
| `internal/handler/public.go` | `base()` 始终注入全部全局数据（RecentPosts、Categories、Tags、ArchiveMonths、RecentCommentItems、SayingCommentItems）；移除 `renderWidgets()` |
| `internal/store/loader.go` | `PopularPosts()` 已移除（改为 functions.goyaegi 实现） |
| `internal/theme/widget.go` | **已删除** |
| `web/themes/default/templates/base.gohtml` | 移除所有 widget define；footer 改为 `{{template "sidebar" .}}` |
| `web/themes/default/templates/sidebar.gohtml` | **新建** — 直接使用全局数据 + `themeInvoke` 调用 |
| `web/themes/default/theme.yaml` | 简化为纯元数据（name/version/description/author） |
| `web/themes/default/functions.goyaegi` | 保留 `popular_posts` 主题函数作为 `themeInvoke` 用法演示 |
| `themes/single/theme.yaml` | 简化为纯元数据，移除 `pages:` 配置 |
| `themes/single/templates/base.gohtml` | **已删除** — 合并到 index.gohtml |
| `themes/single/templates/index.gohtml` | **新建** — 单文件最小主题：内联 header/footer/pagination/comments，用数据特征区分页面类型 |
| `internal/render/render.go` | 移除 `fallbackFromDefaultTheme()`（跨主题 fallback）；新增 `TemplateHierarchy` + `ResolveTemplate()` + `HasTemplate()` + `ResolveFragment()` |
| `internal/handler/public.go` | 所有 `c.HTML` 调用改用 `h.renderer.ResolveTemplate(pageType)`；AJAX fragment 改为通用 `?fragment=<name>` 机制，由 `ResolveFragment()` 解析为 `<name>_fragment.gohtml` |
| `web/assets/comment.js` | `?ajax=comments` 改为 `?fragment=comments` |

### 10.2 待实现

（无）

### 10.3 AJAX Fragment 方案对比

| 方案 | Go 改动 | 主题改动 | JS 改动 | Go 硬编码残留 | 灵活性 | 带宽 |
|------|---------|----------|---------|--------------|--------|------|
| **现状**（硬编码 `fragment_comments.gohtml`） | 已完成 | 定义模板即可 | 无 | `?ajax=comments`、模板名 | 仅评论 | 省 |
| **A**（JS 伪 AJAX，WP 经典做法） | 删除 4 处分支 | 零 | 改 DOM 截取 | `POST /comment`、数据注入 | 无 | 浪费 |
| **B**（通用 Fragment，**已采用**） | ~20 行 | 定义 `fragment_<name>.gohtml` | `?fragment=<name>` | `POST /comment`、数据注入 | 任意 fragment | 省 |
| **C**（主题注册路由，ThemeAPI 大改） | ~200-300 行 | `functions.goyaegi` 注册路由 | 端点 URL 改为主题路径 | **零** | 最高 | 省 |

**选择方案 B 的理由**：改动量小（~20 行 Go），Go 不再硬编码 "comments" 字符串，只认 "fragment" 通用概念。主题可扩展任意 fragment（如 `search_fragment.gohtml`）。方案 C 是终极方案但当前只有评论一个 AJAX 场景，投入产出比不高，等未来有更多动态端点需求时再升级。

### 10.4 Fragment 约定

- 请求参数：`?fragment=<name>`（如 `?fragment=comments`）
- 模板命名：`fragment_<name>.gohtml`（如 `fragment_comments.gohtml`）
- 主题未定义对应模板时，走完整页面渲染（无 AJAX）
- Go 代码不感知 fragment 的具体类型，只做通用解析

### 10.5 说明

- `themeInvoke` 模板函数保留，自定义主题函数继续可用
- 项目处于本地 dev 阶段，无向后兼容负担
