# AGENTS

本文档面向后续进入本仓库的 agent 或开发者，目的是在不通读全仓库的前提下，快速理解项目架构、运行方式、测试边界与修改风险点。

## 1. 项目概览

这是一个 **Go 单体博客应用**，目标是把原 WordPress 博客迁移为 Go Web + SQLite，并 **保持历史永久链接不变**。

- HTTP 服务入口：`cmd/server`
- WordPress WXR 后台导入/导出：`/admin/import` + `internal/importer`
- 运行形态：单二进制 + SQLite + 本地静态目录
- 前端渲染：服务端模板 `html/template`
- Web 框架：Gin
- 数据访问：GORM + `glebarez/sqlite`

核心架构不是传统“按页面拆很多模块”，而是围绕以下几条主线组织：

1. **永久链接兼容**：文章继续使用 `/{year}{id}.html`，页面继续使用 `/{slug}`。
2. **单体分层**：`handler` 负责 HTTP 编排，`store` 负责数据库查询/写入，`render` 负责模板函数与展示规则，`wxr` 负责 WordPress 兼容与内容清洗。
3. **后台导入/导出**：WordPress XML 由后台 `/admin/import` 统一处理；导入与导出逻辑集中在 `internal/importer`，导入按原始 ID upsert 保存。

理解仓库时，优先看这些目录：

- `cmd/server`：服务启动、路由注册、session、中间件、优雅退出
- `internal/handler`：前台/后台 HTTP 处理器
- `internal/store`：数据库查询与写入逻辑
- `internal/importer`：后台 WXR 导入/导出服务
- `internal/model`：GORM 模型与状态常量
- `internal/permalink`：永久链接规则唯一来源
- `internal/render`：模板函数、正文展示规则、代码高亮
- `internal/theme`：主题元数据、激活/预览/恢复、组件和主题选项
- `internal/wxr`：WordPress XML 解析与内容清洗
- `web/templates`、`web/assets`：后台/认证模板与全局静态资源
- `web/themes`、`web/widgets`：内嵌前台主题与内置组件模板

### 请求与数据流

- 服务启动时先加载环境变量配置，再打开 SQLite 并自动迁移。
- Gin 全局中间件顺序为：panic 恢复、Trace ID、访问日志、Prometheus 指标、SQL 追踪、Session、i18n。
- 前台、认证与后台共用同一个进程；认证路由挂在 `/auth/*`，后台管理路由挂在 `/admin/*` 下，后台路由都要求已登录 session。
- handler 层通常只做参数解析、权限判断、调用 store、拼装模板数据；查询细节基本都下沉到 `store`。
- 开发时如果本地存在 `web/templates` 或 `web/assets`，服务会优先直接读取磁盘，便于修改模板/CSS/JS 后立即生效；否则回退到 `embed` 内容。
- 前台展示由主题系统接管：启动时会确保内嵌主题释放到 `themes/`，当前主题由 Setting 表的 `current_theme` 决定；前台模板从 `themes/{name}/templates` 解析，主题资源从 `/theme-assets/*` 输出。

### 主题系统当前模型

- 内嵌主题源文件位于 `web/themes/{default,single}`；运行时会释放到磁盘 `themes/`，已存在的主题不会被覆盖。不要再把前台主题模板放回 `web/templates`。
- 后台/认证模板仍在 `web/templates`；主题模板加载时会补齐后台/认证模板，但前台主题本身应自包含。
- `internal/theme.Manager` 负责扫描 `themes/`、读取 `theme.yaml`、激活/删除/预览主题、加载主题模板、编译 `functions.go/functions.goyaegi`，并在加载失败时回退默认主题。
- `internal/render.Renderer` 负责解析模板、模板函数、模板层级 fallback、预览模板缓存和组件渲染。页面类型到模板的 fallback 链在 `render.TemplateHierarchy` 中维护。
- `theme.yaml` 当前不只是元数据，还声明 `widget_areas`、`widgets`、组件级 `options` 与主题全局 `options`。组件配置保存在 Setting 表的 `widget_<area>`，主题选项保存在 `option_<theme>_<option>`。
- 前台请求通常先通过 `store.DataLoader` 全量预加载公开数据，再由 `Public.base()` 注入 `.RecentPosts`、`.Categories`、`.Tags`、`.ArchiveMonths`、`.RecentCommentItems`、`.Menu` 等通用模板数据。
- 主题自定义数据通过 `functions.goyaegi` 注册 `themeData` provider；普通模板数据优先使用 `base()` 已注入的数据，只有确实需要自定义计算时再用 `themeData`。
- 组件模板命名为 `widget_<id>`，可由主题的 `widgets/{id}.gohtml` 覆盖内置组件；模板中可用 `renderWidgets "area" .` 渲染区域，用 `widgetOption "key"` 读取当前组件实例选项。
- 主题静态资源通过 `/theme-assets/...` 引用，当前/预览主题会自动解析到对应 `assets/` 目录。不要在主题模板里硬编码 `/web/themes/...`。

### 路由设计的关键点

- 前台根路径下只有一个 `/:seg` 单段路由，同时承载：
  - 文章：`/{year}{id}.html`
  - 页面：`/{slug}`
- 目前只有 `archive` 走前台特殊页面逻辑；如果需要“友情链接页”，应创建普通页面并使用 `links` 之类的 slug，而不是依赖单独功能模块。
- 因为文章和页面共享根路径，所以是否为文章完全依赖 `internal/permalink` 解析结果；不要绕过这层自己拼 URL 规则。
- `/?p={id}` 旧 WordPress 链接会尝试 301 到当前永久链接。

### 数据模型的关键约束

- `model.Post.ID` 沿用 WordPress 原始 `post_id`，这是永久链接兼容的核心前提。
- `model.Post` 同时承载文章和页面，通过 `PostType` 区分。
- 当前没有单独的友链数据模型；友情链接若需要保留，应作为普通页面内容维护。
- 评论默认走审核流，状态为 `pending / approved / spam`。
- 上传文件既落磁盘，也在 `upload` 表里记录元数据。

## 2. 构建、运行与常用命令

### 环境要求

- Go 版本：`go 1.25.0`
- 数据库：SQLite（纯 Go 驱动，无需 CGO）

### 设置后台管理员密码

```bash
go run ./cmd/server -reset-password "用户名:密码"
```

如果不传密码部分，程序会生成随机密码并打印。普通首次启动时，如果数据库里还没有任何用户，服务也会自动创建 `admin` 用户并把密码打印到控制台。

### 本地启动开发服务器

```bash
go run ./cmd/server
```

**重启服务**请使用内置的 daemon 模式，不要用 `go run ./cmd/server &` 丢到 bash 后台——bash 会话结束后进程会被杀掉：

```bash
go run ./cmd/server restart
```

程序会自动在后台启动或重启，不依赖当前 shell 会话。

默认访问：

- 前台：`http://localhost:8888/`
- 后台登录：`http://localhost:8888/auth/login`
- 导入：`http://localhost:8888/admin/import`
- 指标：`http://localhost:8888/metrics`
- 健康检查：`http://localhost:8888/healthz`

首次启动且数据库无用户时：

- 会自动创建 `admin` 用户
- 随机密码只打印一次，需从控制台保存
- 后续管理员可以修改自己的用户名, 此时执行 -reset-password admin:xxx 会报错无此用户并列出当前用户

### 导入 / 导出 WordPress XML

登录后台后在 `/admin/import` 进行导入或导出：

- 导入时需要选择“导入归属用户”
- 若 XML 中存在相同 ID 的文章、页面或评论，会按 upsert 覆盖保存
- 导出时可勾选 `文章 / 页面 / 评论 / 设置`，默认全选
- 导出的 XML 可以直接被当前后台重新导入
- 导入逻辑会保留原始 `post_id`，因此仍与永久链接兼容
- 列表分页数量与 feed 输出数量都在后台设置页维护，不再依赖环境变量

### 测试

```bash
go test ./...
```

本仓库已实际验证该命令可通过。

### 构建生产二进制

```bash
go build -o blog ./cmd/server
./blog
```

部署时通常还需要：

- `data/`：SQLite 数据文件目录
- `public/`：历史图片与上传文件目录

说明：模板与 `web/assets` 已通过 `embed` 打包进二进制，但历史图片/上传文件不在 embed 中。

## 3. 代码风格与仓库内约定

以下内容来自现有代码，而不是通用 Go 规范总结。

### 注释与命名

- **注释主要使用中文**，且倾向说明“为什么这样设计”，不是重复代码字面含义。
- **标识符使用英文**，包名短而直接：`store`、`render`、`wxr`、`permalink`、`handler`。
- 常量集中在模型或处理器顶部定义，避免字符串散落。

### 分层约定

- `handler`：负责 HTTP 入参、响应、模板数据拼装、少量权限判断。
- `store`：负责全部数据库语义；新增查询/写入优先放这里，不要把 SQL/GORM 逻辑散落到 handler。
- `render`：负责模板函数、HTML 展示辅助、正文差异化渲染。
- `permalink`：负责文章/页面/分类/标签 URL 规则；不要在别处手写链接格式。
- `wxr`：负责 WordPress 内容兼容与 XML 解析/清洗。
- `internal/importer`：负责后台上传 XML 后的入库、upsert、归属用户绑定，以及可回导 XML 导出。

### 现有实现风格

- 大量使用 **early return**，错误分支尽早退出。
- 错误通常用 `cockroachdb/errors` 包装；新增 store/handler 错误时尽量保持同一风格。
- 视图数据多使用 `gin.H` 拼装，模板字段名采用大写可读键，如 `SiteName`、`CommentPager`、`RecentPosts`。
- 后台保存文章时会同时维护：
  - `ContentMD`：后台编辑原文
  - `Content`：用于前台展示的 HTML
- 评论分页是按"顶层评论"分页，不是按全部评论平铺分页；改评论相关逻辑时先理解这一点。

### 后台表格规范

后台管理页面（文章、评论、分类、标签、附件、用户、主题等）的数据表格统一使用以下模式：

**HTML 结构：**
```gohtml
<div class="table-scroll">
  <table class="data-table {entity}-table">
    <thead>
    <tr>
      <th>{{.t.T "列名"}}</th>
      ...
      <th>{{.t.T "操作"}}</th>
    </tr>
    </thead>
    <tbody>
    {{range .Items}}
    <tr>
      <td data-label="{{$.t.T "列名"}}">{{.Field}}</td>
      ...
      <td class="actions" data-label="{{$.t.T "操作"}}">
        <form class="inline" method="post" action="/admin/...">
          {{template "csrf_field" $}}
          <button type="submit" class="btn-secondary btn-sm">{{$.t.T "操作"}}</button>
        </form>
      </td>
    </tr>
    {{else}}
    <tr><td colspan="N">{{.t.T "暂无内容"}}</td></tr>
    {{end}}
    </tbody>
  </table>
</div>
```

**关键约定：**
- 外层用 `<div class="table-scroll">` 包裹，提供横向滚动
- `<table>` 使用 `class="data-table {entity}-table"`（如 `posts-table`、`comments-table`、`themes-table`）
- 每个 `<td>` 必须带 `data-label` 属性，用于响应式布局
- 操作列使用 `class="actions"`，内嵌 `<form class="inline">` + CSRF token
- 空状态用 `{{else}}` 分支显示 "暂无内容"
- 分页使用 `{{if gt .Pages 1}}` 包裹的 prev/next 链接 + `page-info`

### i18n / 翻译字符串约定

- 源码默认语言是中文；Go 代码优先通过请求级 translator 调用 `tr.T / tr.N / tr.X / tr.XN`，模板里统一使用 `.t.T / .t.N / .t.X / .t.XN`。
- **不要把可翻译句子拆成多个片段再和变量/数字拼接**。像 `T("阅读")` 前面单独放数字、或 `T("文章有") + n + T("篇")` 这类写法都应改成一个完整消息，例如 `N("%d 次阅读", "%d 次阅读", n, n)`。
- **涉及数量时优先用 `N/N64`（或 `XN/XN64`）**，即使中文单复数看起来一样，也要为英文等语言保留复数分支；`int64` 计数统一用 `N64/XN64`。
- **同一中文词在不同语义/词性下可能需要上下文时，优先使用 `X/XN`**。典型例子：列表里的“页面/文章”可能要翻成复数，但“编辑页面/文章”里的对象名通常要用单数；按钮“关闭”与状态“关闭”也可能需要不同译法。
- 带少量 HTML 的翻译文案必须先转义用户输入，再整体做安全输出；不要把未经转义的用户输入直接插进 `safeHTML`。
- 修改翻译调用后，记得运行 `./scripts/update_i18n.sh` 同步 `web/i18n/messages.pot` 与现有 `.po` 文件；如果改了复数/格式化占位符，再额外检查 `msgfmt --check-format`。

### 修改时特别注意

- **不要修改 `Post.ID` 与永久链接之间的关系**，否则会破坏历史链接兼容。
- **不要在 handler 中重新实现 permalink 判断**，统一复用 `internal/permalink`。
- **不要把模板层逻辑搬到前端 JS 里重做**；该项目以服务端渲染为主，JS 主要是增强体验。
- **谨慎执行 `git reset --hard`**：除非已经确认当前工作区的所有改动都有可靠备份且能完整还原，否则不要使用会丢弃改动的命令。
- **不要擅自删除 stash**：stash 应作为恢复兜底保留。需要使用 stash 内容时，优先使用 `git stash apply`，避免 `git stash pop`；更不要执行 `git stash drop`，除非用户明确要求删除对应 stash。

## 4. 测试策略与执行建议

当前测试以标准 `go test` 为主，重点覆盖的是“规则正确性”，而不是完整 HTTP 集成链路。

### 当前已有测试覆盖

- `internal/permalink`：永久链接生成与解析
- `internal/render`：渲染辅助中的头像等纯函数
- `internal/store`：查询语义、分页、评论定位、标签 slug 规则
- `internal/wxr`：WordPress 内容清洗与 `<!--more-->` 等兼容规则

### 当前未覆盖较多的部分

- `internal/handler` 基本没有 HTTP 测试
- `internal/middleware` 没有专门测试
- 登录、上传、评论提交、后台操作等流程缺少集成测试

### 编写新测试时的建议

- 与永久链接、WXR 清洗、评论分页、导入兼容有关的变更，优先补单元测试。
- 数据层测试更适合走“真实 SQLite 临时库”路径，现有测试已经明确避开某些 `:memory:` 细节坑；补测试时尽量沿用同样方式。
- 如果改动 `handler` 且影响状态码、跳转或模板选择，建议补最小可用的 HTTP 测试，而不是只靠手点页面验证。

## 5. 安全与数据保护注意事项

这里仅记录代码里已经能确认的风险点或约束。

### Session 与认证

- session key 后台可修改, 修改后所有登录会话都会失效， 需要重新登录。
- 后台认证依赖 cookie session；当前 cookie 代码只明确设置了 `HttpOnly` 和 `MaxAge`，部署到公网时需要结合反向代理/HTTPS 额外关注 cookie 安全属性。

### 公开暴露的端点

- `/healthz` 是公开路由。
- `/metrics` 通过 Basic Auth 保护，用户名固定为 `metrics`，密码可在后台设置页配置；公网部署仍建议结合网关、反向代理或网络层限制访问来源。

### CSRF 与后台操作

- 当前仓库里可以看到后台和评论提交大量使用普通 POST 表单。
- 已有简单的 CSRF 防护中间件，但每次新增路由时仍需考虑是否有安全问题。

### 评论系统

- 评论默认进入待审，不是直接公开。
- 已有基础反垃圾措施：蜜罐字段、邮箱校验、长度校验、同 IP 限频。
- 当前没有看到验证码或外部反垃圾服务接入。

### Debug 能力

- 后台有一个只读 SQL Debug 页面。
- 代码对 SQL 前缀做了只读限制，但它仍然属于高权限能力；如修改相关逻辑，优先保持“只读”边界，不要放宽。

### 上传文件

- 上传限制了大小和 MIME 白名单。
- 上传文件会落到 `public` 目录下，因此改上传逻辑时要同时考虑：磁盘文件、数据库记录、前台访问路径三者一致性。

## 6. 配置与环境管理

运行时配置来自环境变量，不依赖额外配置文件。

### 主要环境变量

| 变量 | 默认值 | 用途 |
|---|---|---|
| `BLOG_ADDR` | `:8888` | HTTP 监听地址 |
| `BLOG_DB` | `data/blog.db` | SQLite 路径 |
| `BLOG_PUBLIC_DIR` | `public` | 历史图片与上传文件根目录 |
| `BLOG_LOG_JSON` | `false` | 是否输出 JSON 日志 |

### 配置上的关键行为

- RSS 与导出里的绝对链接基于当前请求 Host 生成，不再单独读取 `BLOG_SITE_URL`。
- 前台列表分页数量、feed 输出数量、session secret 都可以在后台设置页修改；其中 session secret 修改后所有登录用户都需要重新登录。
- 启动服务时 `store.Open` 会自动创建数据库目录并执行自动迁移。
- 开发环境下模板与静态资源优先读磁盘，生产环境则通常依赖 embed 资源。

## 7. 调试、重构与排障建议

### SQL 查询分析流程

当需要排查 SQL 查询数量或性能问题时，按以下步骤操作：

1. **访问目标页面**：浏览器访问页面，从响应头中获取 `X-Trace-Id`（如 `bbc51f441ebb4685`）。
2. **从日志中 grep SQL 查询**：
   ```bash
   grep "bbc51f441ebb4685" /path/to/logfile | grep -E "SQL|sql|query"
   ```
   日志中每条 SQL 查询都带有 trace ID，可以精确统计查询次数和内容。
3. **分析重复查询**：按 SQL 语句分组统计，找出重复执行的查询。常见重复来源：
   - `current_theme` 设置被多次读取（每次 `theme.Manager.Current()` 都会查库）
   - Widget 数据（RecentPosts、RecentComments 等）在 `base()` 和 widget `Data()` 中各查一次
   - 模板渲染中通过函数调用触发的隐式查询
4. **优化方向**：缓存可复用的查询结果（如主题名、设置项），将 `base()` 中已查询的数据传递给 widget 复用。

### 需要先看的位置

- 文章/页面访问异常：先看 `internal/permalink`、`cmd/server` 路由注册、`handler/public.go`
- 前台列表/搜索/评论分页异常：先看 `internal/store/query.go`
- 后台写入、文章编辑、评论审核异常：先看 `internal/store/admin.go` 与 `internal/handler/admin.go`
- WordPress 导入/导出兼容问题：先看 `internal/importer` 与 `internal/wxr`
- 模板渲染、`<!--more-->`、代码高亮异常：先看 `internal/render`

### 重构优先级建议

- 可优先重构重复的 handler 数据拼装、后台表单处理与局部辅助函数。
- 不要先动永久链接、导入 ID 兼容、评论分页模型这些“历史兼容基础设施”。

### 改动后最低验证清单

1. `go test ./...`
2. 启动 `go run ./cmd/server`
3. 手动检查：
   - 首页
   - 一篇文章详情页
   - 一个普通页面
   - 评论提交
   - 后台登录
   - 文章保存或预览

## 8. 仓库规则文件状态

当前仓库内**未发现**以下额外规则文件，因此本 AGENTS.md 即为主要仓库级说明：

- `.cursor/rules/`
- `.trae/rules/`
- `.github/copilot-instructions.md`
- 旧版 `AGENT.md` / 已有 `AGENTS.md`

如果未来补充这些规则文件，应优先保持与本文档一致，避免出现冲突指令。
