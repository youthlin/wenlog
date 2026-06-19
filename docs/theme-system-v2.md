# 主题系统 v2 设计方案

## 1. 背景与目标

### 1.1 现状回顾

v1 主题系统已实现：
- 主题 zip 上传/安装/激活/删除
- `theme.yaml` 声明页面数据依赖和 widget 列表
- 8 个内置 Widget（user_info, search, saying, recent_posts, recent_comments, archive_months, categories, tags）
- 模板覆盖机制（主题模板覆盖默认模板）
- 主题翻译支持

### 1.2 v1 局限

- **Widget 数据查询是预定义的**：主题只能使用 8 个内置 Widget，无法自定义数据查询逻辑
- **无法做"相关文章"、"热门文章"、"标签云"等自定义数据块**
- **主题文件只能通过 zip 上传更新**，无法在线编辑微调
- **主题加载失败无恢复机制**：模板解析错误直接导致站点不可用

### 1.3 v2 目标

1. **后台文件编辑器**：在线编辑主题的 `.gohtml`、`.css`、`.js`、`theme.yaml`、`functions.go`
2. **functions.go**：基于 yaegi 解释器的 Go 脚本，主题可注册自定义数据提供者
3. **ThemeAPI**：暴露 DataLoader 全量内存数据的只读视图，主题脚本可自由查询/过滤/排序
4. **恢复机制**：主题加载失败时自动回退到内嵌默认主题

---

## 2. 架构概览

```
┌──────────────────────────────────────────────────────────────┐
│  主题目录 (themes/{name}/)                                    │
│  ├── theme.yaml            # 主题元信息 + 页面配置            │
│  ├── functions.go          # yaegi Go 脚本（可选）            │
│  ├── templates/            # Go 模板 (.gohtml)                │
│  ├── assets/               # 静态资源 (CSS/JS/图片)           │
│  └── i18n/                 # 翻译文件 (.po/.mo)               │
└──────────────────────────────────────────────────────────────┘
         │
         │ 激活时 → Manager.LoadTheme(name)
         ▼
┌──────────────────────────────────────────────────────────────┐
│  ThemeManager.LoadTheme(name)                                │
│  1. 解析 theme.yaml                                          │
│  2. 编译模板（主题模板 + 默认回退 + admin 回退）              │
│  3. 编译 functions.go（yaegi 解释器）                        │
│  4. 调用 Register(api) 注册自定义数据提供者                  │
│  5. 任一步骤失败 → 自动回退到内嵌默认主题                    │
└──────────────────────────────────────────────────────────────┘
         │
         │ 请求时
         ▼
┌──────────────────────────────────────────────────────────────┐
│  Handler                                                      │
│  ├── 模板中调用 {{themeData "popular_posts" "n" 5}}          │
│  ├── 查找已注册的 "popular_posts" 数据提供者                  │
│  ├── 传入 ThemeAPI（DataLoader 只读视图）                     │
│  ├── 执行 functions.go 中的函数，返回数据                     │
│  └── 数据注入模板渲染                                         │
└──────────────────────────────────────────────────────────────┘
```

---

## 3. 后台文件编辑器

### 3.1 功能范围

在后台 **主题管理页** 新增文件编辑器，支持在线编辑当前激活主题的以下文件：

| 文件 | 编辑支持 | 说明 |
|------|---------|------|
| `theme.yaml` | ✅ | YAML 配置 |
| `templates/*.gohtml` | ✅ | Go 模板文件 |
| `assets/*.css` | ✅ | 样式文件 |
| `assets/*.js` | ✅ | 脚本文件 |
| `functions.go` | ✅ | 自定义数据提供者（Go 脚本） |
| `i18n/*.po` | ✅ | 翻译文件 |

### 3.2 安全约束

- **路径穿越防护**：编辑的文件路径必须在 `themes/{name}/` 目录内
- **文件类型白名单**：只允许编辑 `.gohtml`、`.css`、`.js`、`.yaml`、`.yml`、`.po`、`.mo`、`.go`（仅 functions.go）
- **大小限制**：单文件最大 100KB
- **functions.go 特殊处理**：保存后触发主题重新加载（含 yaegi 编译），失败则回退默认主题并提示错误

### 3.3 API 设计

```
GET  /admin/theme/files              → 列出当前主题文件树
GET  /admin/theme/file?path=...      → 读取文件内容
POST /admin/theme/file               → 保存文件内容 {path, content}
POST /admin/theme/file/delete        → 删除文件（仅非核心文件）
```

### 3.4 前端 UI

在 `admin_themes.gohtml` 页面增加"编辑文件"标签页：
- 左侧：文件树（可折叠目录）
- 右侧：代码编辑器（Monaco Editor 或简易 textarea，带语法高亮）
- 保存按钮 + 保存并重载按钮（functions.go 修改后需重载）

---

## 4. functions.go 设计

### 4.1 为什么选 yaegi

- **纯 Go 解释器**：无需 CGO，交叉编译友好
- **支持 Go 标准库子集**：`fmt`、`sort`、`strings`、`time` 等
- **类型安全**：编译期检查语法，运行时检查类型
- **沙箱执行**：可限制可用的包和函数
- **Traefik 生产使用**：成熟度有保障

### 4.2 functions.go 规范

主题在根目录放置 `functions.go`，内容为合法的 Go 源码：

```go
// functions.go - 主题自定义数据提供者
// 必须定义 Register 函数，接收 *theme.API 参数

package theme

import (
    "sort"
)

func Register(api *API) {
    // 注册"热门文章"数据提供者
    api.RegisterData("popular_posts", func(args map[string]any) any {
        n := 5
        if v, ok := args["n"]; ok {
            if nv, ok := v.(int); ok && nv > 0 {
                n = nv
            }
        }
        posts := api.Posts() // 获取全部已发布文章
        // 按浏览量排序
        sorted := make([]PostMeta, 0, len(posts))
        for _, p := range posts {
            sorted = append(sorted, p)
        }
        sort.Slice(sorted, func(i, j int) bool {
            return sorted[i].Views > sorted[j].Views
        })
        if n > len(sorted) {
            n = len(sorted)
        }
        return sorted[:n]
    })

    // 注册"标签云"数据提供者
    api.RegisterData("tag_cloud", func(args map[string]any) any {
        tags := api.Tags()
        type TagCloudItem struct {
            Name  string
            Slug  string
            Count int
            Size  string // "xs", "sm", "md", "lg", "xl"
        }
        // ... 计算标签云逻辑
        return items
    })

    // 注册"相关文章"数据提供者
    api.RegisterData("related_posts", func(args map[string]any) any {
        currentPostID := args["post_id"].(uint)
        n := 5
        if v, ok := args["n"]; ok {
            if nv, ok := v.(int); ok && nv > 0 {
                n = nv
            }
        }
        // 基于标签相似度计算相关文章
        // ...
        return related
    })
}
```

### 4.3 注册与执行生命周期

```
主题激活/加载时：
  1. 读取 functions.go 文件内容
  2. yaegi 编译（Eval）
  3. 查找 Register 函数
  4. 创建 *theme.API 实例（持有 DataLoader 引用）
  5. 调用 Register(api)
  6. api 内部将注册的函数存入 map[string]DataProvider
  7. 编译失败 → 日志记录 + 回退默认主题

请求时：
  1. 模板调用 {{themeData "popular_posts" "n" 5}}
  2. themeData 函数查找已注册的 "popular_posts"
  3. 解析参数为 map[string]any{"n": 5}
  4. 调用 DataProvider(args)
  5. 返回数据注入模板上下文
  6. 执行出错 → 返回 nil（优雅降级，不中断页面渲染）
```

### 4.4 模板函数

新增模板函数 `themeData`：

```go
// 模板中调用
{{themeData "provider_name" "key1" value1 "key2" value2}}

// 示例
{{range themeData "popular_posts" "n" 10}}
    <li><a href="{{postURL .}}">{{.Title}}</a> ({{.Views}} 阅读)</li>
{{end}}

{{range themeData "related_posts" "post_id" .Post.ID "n" 5}}
    ...
{{end}}
```

实现：
```go
func themeData(name string, args ...any) any {
    // 1. 查找注册的 DataProvider
    provider := registeredProviders[name]
    if provider == nil {
        return nil
    }
    // 2. 解析 key-value 参数对
    parsed := parseKVArgs(args)
    // 3. 执行
    return provider(parsed)
}
```

---

## 5. ThemeAPI 设计

### 5.1 核心原则

**ThemeAPI 不预定义接口**（如 `PopularPosts()`、`RelatedPosts()`），而是**暴露 DataLoader 的全量内存数据只读视图**。主题在 `functions.go` 中自行编写过滤、排序、聚合逻辑。

### 5.2 API 结构

```go
// internal/theme/api.go

// API 是暴露给主题脚本的只读数据视图。
// 每个请求共享同一个 API 实例（持有 DataLoader 引用），
// 但 DataProvider 函数每次请求都会重新执行。
type API struct {
    loader *store.DataLoader
    providers map[string]DataProvider
}

// DataProvider 是 functions.go 中注册的数据提供函数。
type DataProvider func(args map[string]any) any

// RegisterData 注册一个命名数据提供者。
func (api *API) RegisterData(name string, fn DataProvider) {
    api.providers[name] = fn
}
```

### 5.3 暴露的数据访问方法

```go
// --- 文章 ---

// Posts 返回全部已发布文章（按发布时间倒序）。
func (api *API) Posts() []PostView

// Post 按 ID 获取单篇文章。
func (api *API) Post(id uint) *PostView

// Pages 返回全部已发布页面。
func (api *API) Pages() []PostView

// PageBySlug 按 slug 获取页面。
func (api *API) PageBySlug(slug string) *PostView

// RecentPosts 返回最近 n 篇文章。
func (api *API) RecentPosts(n int) []PostView

// PostsByCategory 返回指定分类下的文章。
func (api *API) PostsByCategory(categorySlug string) []PostView

// PostsByTag 返回指定标签下的文章。
func (api *API) PostsByTag(tagSlug string) []PostView

// PostsByYear 返回指定年份的文章。
func (api *API) PostsByYear(year int) []PostView

// PostsByYearMonth 返回指定年月的文章。
func (api *API) PostsByYearMonth(year, month int) []PostView

// --- 分类 & 标签 ---

// Categories 返回全部分类。
func (api *API) Categories() []CategoryView

// Tags 返回全部标签。
func (api *API) Tags() []TagView

// --- 评论 ---

// CommentsByPost 返回指定文章的已批准评论。
func (api *API) CommentsByPost(postID uint) []CommentView

// RecentComments 返回最近 n 条已批准评论。
func (api *API) RecentComments(n int) []CommentView

// --- 用户 ---

// Users 返回全部用户。
func (api *API) Users() []UserView

// User 按 ID 获取用户。
func (api *API) User(id uint) *UserView

// --- 设置 ---

// Setting 读取设置项。
func (api *API) Setting(key string) string

// Settings 批量读取设置项。
func (api *API) Settings(keys ...string) map[string]string

// --- 归档 ---

// ArchiveMonths 返回归档月份统计。
func (api *API) ArchiveMonths() []ArchiveMonthView

// --- 工具 ---

// PostURL 生成文章永久链接。
func (api *API) PostURL(p PostView) string

// PageURL 生成页面永久链接。
func (api *API) PageURL(p PostView) string

// CategoryURL 生成分类永久链接。
func (api *API) CategoryURL(slug string) string

// TagURL 生成标签永久链接。
func (api *API) TagURL(slug string) string
```

### 5.4 View 类型（只读投影）

为避免主题脚本直接修改原始数据，API 返回的是**值类型的 View 结构体**（拷贝），而非指针：

```go
// PostView 是文章的只读视图。
type PostView struct {
    ID           uint
    Title        string
    Slug         string
    Excerpt      string
    Content      string // HTML 正文
    AuthorID     uint
    Status       string
    PostType     string // "post" or "page"
    Views        int64
    MenuOrder    int
    PublishedAt  time.Time
    ModifiedAt   time.Time
    CommentCount int64
    Author       UserView
    Categories   []CategoryView
    Tags         []TagView
}

type CategoryView struct {
    ID          uint
    Name        string
    Slug        string
    Description string
    ParentID    uint
    PostCount   int64
}

type TagView struct {
    ID        uint
    Name      string
    Slug      string
    PostCount int64
}

type CommentView struct {
    ID        uint
    PostID    uint
    ParentID  uint
    Author    string
    Content   string
    Status    string
    CreatedAt time.Time
}

type UserView struct {
    ID          uint
    Username    string
    DisplayName string
    Email       string
    Role        string
}

type ArchiveMonthView struct {
    Year  int
    Month int
    Count int64
}
```

### 5.5 为什么用 View 类型而非直接暴露 model

1. **安全隔离**：View 是值拷贝，主题脚本无法修改原始数据
2. **字段裁剪**：不暴露敏感字段（PasswordHash、IP 等）
3. **API 稳定性**：model 变更不影响主题 API
4. **yaegi 兼容**：yaegi 对简单 struct 的支持更好

---

## 6. 恢复机制

### 6.1 触发条件

以下任一情况触发回退：
- `theme.yaml` 解析失败
- 模板编译失败（`parseTemplates` 返回 error）
- `functions.go` yaegi 编译失败
- `Register(api)` 调用 panic

### 6.2 回退流程

```
Manager.LoadTheme(name)
  │
  ├── 1. LoadTheme(dir) → 解析 theme.yaml
  │     └── 失败 → fallbackToDefault()
  │
  ├── 2. renderer.LoadTheme(templatesDir)
  │     └── 失败 → fallbackToDefault()
  │
  ├── 3. 编译 functions.go（如果存在）
  │     ├── yaegi.Eval(functionsGoSource)
  │     ├── 查找 Register 函数
  │     ├── 创建 API 实例
  │     ├── 调用 Register(api)
  │     └── 任一步失败 → 日志记录 + fallbackToDefault()
  │
  └── 4. 成功 → 更新 currentTheme + 记录成功日志

fallbackToDefault():
  1. 日志记录错误详情（含堆栈）
  2. 设置 currentTheme = "default"
  3. renderer.ResetToDefault()
  4. 清空自定义 DataProvider 注册表
  5. 在后台显示警告横幅："主题 xxx 加载失败，已自动切换为默认主题。错误：..."
```

### 6.3 后台通知

在后台页面顶部显示可关闭的警告横幅：
- 主题名 + 错误信息
- "编辑文件修复"按钮 → 跳转到文件编辑器
- "重试加载"按钮 → 重新尝试加载当前主题

### 6.4 前台行为

- 回退后前台无感知，正常使用默认主题渲染
- 不显示任何错误信息给访客
- 仅管理员在后台看到警告

---

## 7. 实现计划

### 7.1 新增文件

| 文件 | 说明 |
|------|------|
| `internal/theme/api.go` | ThemeAPI 定义：API 结构体、View 类型、数据访问方法 |
| `internal/theme/functions.go` | functions.go 编译与执行：yaegi 集成、Register 调用、DataProvider 管理 |
| `internal/theme/recovery.go` | 恢复机制：fallbackToDefault、错误记录、后台通知 |
| `internal/handler/admin_theme_files.go` | 文件编辑器 handler：列出/读取/保存/删除主题文件 |

### 7.2 修改文件

| 文件 | 改动 |
|------|------|
| `internal/theme/manager.go` | `LoadTheme` 方法整合模板加载 + functions.go 编译 + 恢复逻辑 |
| `internal/render/render.go` | 新增 `themeData` 模板函数注册 |
| `internal/handler/admin_theme.go` | 增加文件编辑器路由注册 |
| `internal/handler/public.go` | `base()` 注入 ThemeAPI 到模板数据；`renderWidgets` 支持自定义 DataProvider |
| `cmd/server/routes.go` | 注册文件编辑器路由 |
| `cmd/server/web.go` | 初始化时注入 DataLoader 到 ThemeAPI |
| `web/templates/admin_themes.gohtml` | 增加文件编辑器 UI |
| `go.mod` | 添加 `github.com/traefik/yaegi` 依赖 |

### 7.3 实施步骤

1. **ThemeAPI 定义**（`internal/theme/api.go`）
   - View 类型定义
   - API 结构体 + 数据访问方法
   - DataProvider 类型 + RegisterData

2. **functions.go 编译与执行**（`internal/theme/functions.go`）
   - yaegi 解释器初始化
   - 编译 functions.go 源码
   - 调用 Register(api)
   - 模板函数 `themeData` 注册

3. **恢复机制**（`internal/theme/recovery.go`）
   - `LoadTheme` 方法（整合加载 + 恢复）
   - 错误日志 + 后台通知数据

4. **Manager 改造**（`internal/theme/manager.go`）
   - 新增 `LoadTheme(name string) error` 方法
   - 整合模板加载 + functions.go 编译 + 恢复

5. **Renderer 扩展**（`internal/render/render.go`）
   - 注册 `themeData` 模板函数
   - 模板函数查找 DataProvider 并执行

6. **文件编辑器**（`internal/handler/admin_theme_files.go`）
   - 列出文件树
   - 读取/保存/删除文件
   - functions.go 保存后触发重载

7. **路由 + 初始化**
   - 注册文件编辑器路由
   - 初始化时关联 DataLoader 到 ThemeAPI

8. **后台 UI**
   - 文件编辑器界面
   - 恢复警告横幅

---

## 8. 风险与注意事项

### 8.1 yaegi 限制

- **性能**：解释执行比原生 Go 慢 10-100 倍，但博客场景数据量小（< 1 万篇文章），可接受
- **包支持**：yaegi 不支持所有 Go 标准库，需测试确认 `sort`、`strings`、`time` 等常用包可用
- **并发安全**：yaegi 的解释器实例不是并发安全的，需加锁或每个请求创建新实例

### 8.2 安全考量

- **functions.go 只能通过后台编辑器修改**（需管理员权限），不上传
- **yaegi 沙箱**：限制可导入的包（白名单：`fmt`、`sort`、`strings`、`time`、`math` 等安全包）
- **禁止的包**：`os`、`os/exec`、`net`、`net/http`、`runtime`、`syscall`、`reflect`、`unsafe` 等
- **执行超时**：DataProvider 执行设置 1 秒超时，防止死循环

### 8.3 兼容性

- **现有主题不受影响**：没有 `functions.go` 的主题行为与 v1 完全一致
- **内置 Widget 保留**：8 个内置 Widget 继续可用，与自定义 DataProvider 共存
- **模板向后兼容**：`themeData` 是新模板函数，旧模板不使用则无影响

---

## 9. 示例：完整主题

```
themes/my-blog/
├── theme.yaml
├── functions.go
├── templates/
│   ├── base.gohtml
│   ├── index.gohtml
│   ├── post.gohtml
│   └── widget_popular_posts.gohtml   # 自定义 widget 模板
├── assets/
│   └── style.css
└── i18n/
    └── zh_CN.po
```

**theme.yaml**:
```yaml
name: "my-blog"
version: "2.0"
description: "自定义博客主题"
author: "youthlin"

pages:
  index:
    data:
      - PostList
    widgets:
      - popular_posts    # 自定义 widget
      - tag_cloud        # 自定义 widget
      - categories
  post:
    data:
      - Post
      - Comments
    widgets:
      - related_posts    # 自定义 widget
```

**functions.go**（见第 4.2 节示例）

**widget_popular_posts.gohtml**:
```gohtml
{{define "widget_popular_posts"}}
<section class="widget popular-posts">
  <h3>{{.t.T "热门文章"}}</h3>
  <ol>
    {{range themeData "popular_posts" "n" 10}}
      <li>
        <a href="{{postURL .}}">{{.Title}}</a>
        <span class="views">({{.Views}} 阅读)</span>
      </li>
    {{end}}
  </ol>
</section>
{{end}}
```

注意：自定义 widget 的模板名约定为 `widget_{name}`（与内置 widget 一致），但数据不是通过 `Widget.Data()` 获取，而是通过模板中的 `{{themeData "name"}}` 直接调用。
