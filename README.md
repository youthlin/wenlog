# 霖博客 (Go 单体应用)

将原 WordPress 博客 [youthlin.com](https://youthlin.com) 复刻为 Go 单体应用(Go Web + SQLite),
抛弃 WordPress 插件体系,只保留个人博客核心功能,**保持原有永久链接不变**。

## 技术栈

- Web:[gin](https://github.com/gin-gonic/gin)
- ORM:[gorm](https://gorm.io) + [glebarez/sqlite](https://github.com/glebarez/sqlite)(纯 Go,免 CGO)
- 模板:`html/template`(服务端渲染)
- 日志:`log/slog`
- 错误:[cockroachdb/errors](https://github.com/cockroachdb/errors)
- 监控:[prometheus/client_golang](https://github.com/prometheus/client_golang)
- 会话:gin-contrib/sessions(cookie) + bcrypt
- Markdown:[gomarkdown](https://github.com/gomarkdown/markdown)

## 永久链接规则(保持不变)

| 类型 | 规则 | 示例 |
|------|------|------|
| 文章 | `/{发布年份}{post_id}.html` | `/20128.html` |
| 页面 | `/{slug}` | `/about` |
| 分类 | `/category/{slug}` | `/category/go` |
| 标签 | `/tag/{slug}` | `/tag/ajax` |
| 旧链接 | `/?p={id}` → 301 | → `/20128.html` |

## 目录结构

```
cmd/server      HTTP 服务入口
internal/
  config        环境变量配置
  model         gorm 模型
  store         DB 连接、迁移、查询
  importer      后台 WXR 导入/导出服务
  wxr           WordPress XML 解析 + 内容清洗
  permalink     永久链接生成/解析(前台与重定向共用)
  handler       前台 + 后台 handler
  middleware    日志/prometheus/recover/认证
  render        模板加载与函数
web/
  templates     *.gohtml(嵌入二进制)
  assets        css/js(嵌入二进制)
public/wp-content/uploads   历史图片(原路径静态服务)
data/blog.db    SQLite(运行时生成)
```

## 快速开始

### 1. 启动服务

```bash
go run ./cmd/server
# 默认监听 :8888
```

- 若数据库中还没有任何用户，程序会自动创建一个 `admin` 用户，并把随机密码打印到控制台
- 前台:http://localhost:8888/
- 后台:http://localhost:8888/admin/login
- 监控:http://localhost:8888/metrics

### 2. 可选:手动重置管理员密码

```bash
go run ./cmd/server -set-admin "youthlin:你的密码"
```

### 3. 在后台导入 / 导出 WordPress 数据

登录后台后打开 `http://localhost:8888/admin/import`：

- 导入：上传 WXR XML 文件，选择将导入内容归属给哪个后台用户
- 导入时会提示：若已存在相同 ID 的文章/页面/评论，将按 upsert 覆盖保存
- 导出：可勾选导出 `文章 / 页面 / 评论 / 设置`，默认全选，导出 XML 可直接在本页重新导入

导入完成后页面会展示文章 / 页面 / 评论 / 分类 / 标签 / 设置统计。

## 配置(环境变量)

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `BLOG_ADDR` | `:8888` | 监听地址 |
| `BLOG_DB` | `data/blog.db` | SQLite 路径 |
| `BLOG_PUBLIC_DIR` | `public` | 静态图片目录(含 wp-content) |
| `BLOG_SITE_NAME` | `霖博客` | 站点标题 |
| `BLOG_SESSION_SECRET` | `change-me-in-production` | 启动时的 session secret 默认值; 后续可在后台修改 |
| `BLOG_LOG_JSON` | `false` | 是否 JSON 日志 |

说明:

- `SiteURL` 不再单独配置, RSS/导出等绝对链接基于当前请求 Host 生成
- 列表分页数量、Feed 输出数量已改为后台设置项,不再通过环境变量控制

## 功能

- 首页 / 文章详情 / 分类 / 标签 / 归档 / 独立页面
- `<!--more-->` 支持:列表页只显示开头 + 「阅读全文」,详情页完整内容
- 评论:开放访客提交 + 审核制(待审队列)+ 楼中楼 + 基础防 spam(蜜罐 / 限频 / 校验)+ Ajax 提交
- 后台:登录、文章/页面 Markdown 增删改、评论审核、导入导出、附件与基础设置管理
- RSS:`/feed`
- 监控:Prometheus `/metrics`(QPS / 延迟 / 状态码)+ slog 访问日志
- 优雅退出

## 部署

单一二进制 + `data/` 目录 + `public/` 目录即可运行:

```bash
go build -o blog ./cmd/server
BLOG_SESSION_SECRET=$(openssl rand -hex 16) ./blog
```

模板与 css/js 已通过 `embed` 打包进二进制,无需随附。

## 测试

```bash
go test ./...
```
