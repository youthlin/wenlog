# 主题系统设计方案

> 历史设计稿：当前实现以 `AGENTS.md`、`docs/template-data-reference.md` 和 `docs/theme-system-optimization-plan.md` 为准。

## 1. 诉求

### 1.1 核心需求

- 管理员在后台**上传一个 zip 压缩包**，就能给博客换一套全新的外观布局
- 不同主题可以在**不同页面**使用不同的 widget 布局（如首页展示 widget、文章页不展示 widget，或把 widget 放在页脚）
- 主题可以声明自己需要哪些数据，**按需查询**，避免无条件查询所有 widget 数据
- 主题可以覆盖 widget HTML，只需要声明页面要渲染哪些 widget；Go 代码负责提供数据

### 1.2 设计原则

- **数据与视图分离**：模板不直接访问 DB，只消费 Go 代码组装好的 ViewModel
- **安全**：上传的模板在服务端由 `html/template` 执行，自带 XSS 防护；zip 解压需防路径穿越
- **渐进式**：先实现主题切换（模板 + 静态资源），内置 Widget 负责取数，默认主题模板提供 HTML，主题可按需覆盖

---

## 2. 整体架构

```
┌──────────────────────────────────────────────────┐
│  主题 Zip 包                                     │
│  ├── theme.yaml        # 主题元信息 + 页面配置   │
│  ├── templates/        # Go 模板文件 (.gohtml)   │
│  │   ├── base.gohtml                             │
│  │   ├── index.gohtml                            │
│  │   ├── post.gohtml                             │
│  │   ├── page.gohtml                             │
│  │   ├── list.gohtml                             │
│  │   ├── archive.gohtml                          │
│  │   └── error.gohtml                            │
│  ├── i18n/             # 主题翻译 (.po/.mo)      │
│  │   └── en_US.po                                │
│  └── assets/           # 静态资源 (CSS/JS/图片)  │
│      ├── style.css                               │
│      └── theme.js                                │
└──────────────────────────────────────────────────┘
         │
         │ 上传 → 校验 → 解压到 themes/{name}/
         ▼
┌──────────────────────────────────────────────────┐
│  ThemeManager                                    │
│  ├── 扫描 themes/ 目录                           │
│  ├── 安装/激活/删除主题                          │
│  ├── 以主题 name 为 domain 加载 i18n/ 翻译       │
│  └── 持久化 current_theme 到 Setting 表          │
└──────────────────────────────────────────────────┘
         │
         │ 激活时 → LoadTheme(dir)
         ▼
┌──────────────────────────────────────────────────┐
│  Renderer (internal/render)                      │
│  ├── 先解析 embed 默认模板                       │
│  ├── 再解析主题模板（同名覆盖）                  │
│  └── 主题未提供的模板自动回退默认                │
└──────────────────────────────────────────────────┘
         │
         │ 渲染时
         ▼
┌──────────────────────────────────────────────────┐
│  Handler (internal/handler)                      │
│  ├── 读 theme.yaml 当前页面的 data/widgets 配置  │
│  ├── 按需查询数据（只查声明的字段）              │
│  ├── 按需渲染 widget（优先使用主题模板）         │
│  └── 注入 gin.H → 模板渲染                       │
└──────────────────────────────────────────────────┘
```

---

## 3. theme.yaml 配置规范

### 3.1 完整示例

```yaml
# themes/my-theme/theme.yaml
name: "极简白"
version: "1.0"
description: "一个干净明亮的主题"
author: "youthlin"

# 按页面声明数据依赖和 widget 列表
pages:
  index:                          # 首页
    data:                         # 需要的数据字段
      - PostList                  # 文章列表（含分页信息）
      - RecentPosts               # 近期文章
    widgets:                      # widget 列表
      - recent_posts
      - categories

  post:                           # 文章详情页
    data:
      - Post                      # 文章内容
      - Comments                  # 评论列表
      - PrevPost                  # 上一篇
      - NextPost                  # 下一篇
    widgets: []                   # 无 widget

  page:                           # 独立页面
    data:
      - Post
      - Comments
    widgets: []

  list:                           # 搜索/分类/标签列表页
    data:
      - PostList
      - Categories
      - Tags
    widgets:
      - categories
      - tags

  archive:                        # 归档页
    data:
      - Groups                    # 按年份分组的文章
    widgets:
      - archive_months

  error:                          # 404/500 错误页
    data: []
    widgets: []
```

### 3.2 预定义数据字段

| 字段 | 类型 | 说明 | 适用页面 |
|------|------|------|----------|
| `PostList` | `*store.ListPostsResult` | 文章列表 + 分页 | index, list |
| `Post` | `*model.Post` | 单篇文章/页面 | post, page |
| `Comments` | `[]*model.Comment` | 评论列表 | post, page |
| `PrevPost` | `*model.Post` | 上一篇文章 | post |
| `NextPost` | `*model.Post` | 下一篇文章 | post |
| `RecentPosts` | `[]*model.Post` | 近期文章 | index, widget |
| `RecentComments` | `[]*model.Comment` | 近期评论 | widget |
| `Categories` | `[]*model.Term` | 分类列表 | widget |
| `Tags` | `[]*model.Term` | 标签列表 | widget |
| `ArchiveMonths` | `[]*model.ArchiveMonth` | 归档月份 | widget |
| `Groups` | `[]group{Year, Posts}` | 按年分组 | archive |
| `SayingComments` | `[]*model.Comment` | 博主动态 | widget |

### 3.3 预定义 Widget

| Widget 名 | 对应数据字段 | 说明 |
|-----------|-------------|------|
| `recent_posts` | `RecentPosts` | 近期文章列表 |
| `recent_comments` | `RecentComments` | 近期评论列表 |
| `categories` | `Categories` | 分类列表 |
| `tags` | `Tags` | 标签列表 |
| `archive_months` | `ArchiveMonths` | 归档月份 |
| `saying` | `SayingComments` | 博主动态 |
| `search` | 无（纯 HTML 表单） | 搜索框 |
| `user_info` | `CurrentUser` | 用户信息/登录入口 |

---

## 4. 模板加载策略：合并加载

```
加载顺序：
  1. 解析 embed 默认模板（web/templates/*.gohtml）
  2. 解析主题模板（themes/{name}/templates/*.gohtml）
  → 同名模板后者覆盖前者
  → 主题未提供的模板自动回退默认
```

### 4.1 模板命名约定

主题模板必须与默认模板**同名**才能覆盖：

| 默认模板 | 说明 | 主题可覆盖 |
|----------|------|-----------|
| `base.gohtml` | 公共布局（header/footer/widgets） | ✅ |
| `index.gohtml` | 首页 | ✅ |
| `post.gohtml` | 文章详情 | ✅ |
| `page.gohtml` | 页面详情 | ✅ |
| `list.gohtml` | 搜索/分类/标签列表 | ✅ |
| `archive.gohtml` | 归档页 | ✅ |
| `error.gohtml` | 错误页 | ✅ |
| `admin_*.gohtml` | 后台模板 | 一般不覆盖 |

### 4.2 模板可用的数据

所有页面共享的基础数据（`h.base()` 注入）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `SiteName` | `string` | 站点名称 |
| `Title` | `string` | 页面标题 |
| `Description` | `string` | 页面描述 |
| `Menu` | `[]*model.Post` | 导航菜单页面列表 |
| `CurrentYear` | `int` | 当前年份 |
| `CurrentUserID` | `uint` | 当前登录用户 ID（0=未登录） |
| `CurrentUser` | `*model.User` | 当前登录用户 |
| `CSRFToken` | `string` | CSRF 令牌 |
| `RegistrationOpen` | `bool` | 是否开放注册 |
| `MailEnabled` | `bool` | 是否启用邮件 |
| `Widgets` | `[]template.HTML` | widget HTML 片段列表 |
| `t` | `translator` | i18n 翻译器 |
| `langURL` | `string` | 语言切换 URL |
| `htmlLang` | `string` | HTML lang 属性值 |

各页面额外数据由 `theme.yaml` 的 `pages.{page}.data` 声明，handler 按需注入。

### 4.3 模板函数

所有模板函数由 `internal/render` 注册，主题模板可直接使用：

- `postURL`, `categoryURL`, `tagURL` — 永久链接生成（`postURL` 同时支持文章和页面）
- `safeHTML`, `escapeHTML` — HTML 安全输出
- `postExcerptHTML`, `detailHTML` — 正文渲染（摘要/完整）
- `hasMore` — 判断是否有 `<!--more-->`
- `avatarURL` — 头像
- `fmtDate`, `fmtDateTime`, `fmtFileSize` — 格式化
- `year`, `add`, `sub`, `seq` — 辅助函数

### 4.4 主题翻译

主题模板里的可见文案仍统一写 `.t.T` / `.t.N` / `.t.X` 等调用，但 `.t` 会被注入为当前主题 domain 的翻译器：

```gohtml
<h3>{{.t.T "Recent Posts"}}</h3>
```

加载约定：

- 主题包可提供 `i18n/` 目录，放置 `.po` 或 `.mo` 文件，例如 `i18n/en_US.po`。
- 文本域（domain）使用 `theme.yaml` 的 `name` 值；例如主题 `name: "single"` 会绑定到 `single` domain。
- Go 侧渲染页面和 widget 时注入 `t = i18n.Get(c).D(themeName)`，因此主题模板无需写 domain，仍然使用 `.t.T`。
- 默认主题名是 `default`，会继续使用应用默认 domain 的翻译。
- 如果主题没有提供某个语言的翻译，gettext 会回退到模板源码中的原文。

---

## 5. 安全设计

### 5.1 Zip 上传安全

- **大小限制**：5MB
- **结构校验**：必须包含 `theme.yaml` + `templates/` 目录
- **路径穿越防护**：解压时校验每个文件路径，拒绝 `../` 等穿越路径
- **文件类型白名单**：只允许 `.gohtml`、`.html`、`.css`、`.js`、`.json`、`.yaml`、`.yml`、`.po`、`.mo`、图片与字体等静态资源
- **theme.yaml 校验**：解析 YAML，校验 `name` 字段必填且不含路径分隔符

### 5.2 模板执行安全

- Go 的 `html/template` 自动转义输出，防止 XSS
- 模板在服务端执行，无法访问文件系统或执行系统命令
- `missingkey=zero`：访问不存在的字段不会 panic，渲染零值

### 5.3 静态资源隔离

- 主题 assets 通过 `/theme-assets/{name}/` 路由提供
- 使用 `http.FileServer` + `http.StripPrefix`，限制在主题目录内

---

## 6. 实现计划

### 6.1 新增文件

| 文件 | 说明 |
|------|------|
| `internal/theme/theme.go` | Theme 结构体 + theme.yaml 解析 |
| `internal/theme/manager.go` | 扫描/安装/激活/删除主题 |
| `internal/theme/widget.go` | Widget 接口 + 注册表 + 内置 widget |
| `internal/handler/admin_theme.go` | 上传/激活/删除主题的 HTTP handler |
| `themes/default/theme.yaml` | 默认主题配置（兼容现有行为） |

### 6.2 修改文件

| 文件 | 改动 |
|------|------|
| `internal/render/render.go` | 新增 `LoadTheme(dir)` 和 `ResetToDefault()` 方法 |
| `internal/handler/public.go` | `base()` 按 theme.yaml 按需查询；各 handler 按页面配置注入数据 |
| `cmd/server/routes.go` | 注册主题管理路由 + `/theme-assets/` 静态资源路由 |
| `cmd/server/web.go` | 初始化 ThemeManager，注入 renderer |
| `web/templates/admin_settings.gohtml` | 开发设置区加主题管理 UI（上传/激活/删除） |

### 6.3 实施步骤

1. **`internal/theme/` 包**：Theme 结构体、theme.yaml 解析、Manager
2. **`internal/render` 扩展**：`LoadTheme` / `ResetToDefault`
3. **`internal/handler/admin_theme.go`**：上传/激活/删除 handler
4. **路由 + 初始化**：注册路由、初始化 Manager
5. **`internal/handler/public.go` 改造**：按页面配置按需查询数据
6. **后台 UI**：设置页加主题管理面板
7. **默认主题配置**：`themes/default/theme.yaml` 保持现有行为

---

## 7. Widget HTML 覆盖机制

内置 Widget 仍由 Go 代码注册并负责取数，但 HTML 不再只能由 `builtin_widgets.go` 固定输出：主题可以在任意 `.gohtml` 文件中定义对应的命名模板来覆盖输出结构。

```go
// internal/theme/widget.go
type Widget interface {
    Name() string
    Data(ctx context.Context, store *store.Store, settings WidgetSettings) (any, error)
}

var registry = map[string]Widget{}

func Register(name string, w Widget) {
    registry[name] = w
}
```

渲染规则：

1. `theme.yaml` 的 `pages.{page}.widgets` 声明要渲染哪些 widget。
2. Handler 找到对应 Widget 后，先检查当前模板集中是否存在 `widget_{name}` 命名模板。
3. 调用 Widget 的 `Data()` 取得数据，并注入 `.t.T` 等 i18n 字段后执行模板。
4. 默认模板已提供全部内置 widget 的 `widget_{name}` 实现；主题可以定义同名模板覆盖 HTML。

主题覆盖示例：

```gohtml
{{define "widget_recent_posts"}}
<section class="widget widget-recent-posts custom-recent-posts">
  <h3>最新文章</h3>
  <ol>
    {{range .Posts}}<li><a href="{{postURL .}}">{{.Title}}</a></li>{{end}}
  </ol>
</section>
{{end}}
```

这样主题提供方只需要在 `widgets` 中声明需要哪些数据块，并按约定模板名写 HTML；Go 侧继续负责权限、查询和默认数据组装，模板侧通过 `postURL` / `categoryURL` / `.t.T` 等函数处理链接与翻译。
