# 待办事项

> 2026-06-20 仓库探索产出，按优先级排列。

## 🔴 高优先级

- [x] **补充 `site_logo` 设置** — 后台缺少站点 Logo 配置项，当前只能硬编码在模板里
- [x] **搜索未用 DataLoader 加速** — 搜索直接走 SQLite LIKE，没享受内存 DataLoader 的 0-SQL 优势
- [x] **Sitemap 未实现** — 路由已注册但 handler 未写，搜索引擎无法发现站点

## 🟡 中优先级

- [x] **Handler 层 HTTP 测试缺失** — 登录、文章 CRUD、评论审核、设置保存等核心流程无自动化回归
- [x] **Middleware 无测试** — CSRF、Session、认证中间件无覆盖
- [x] **主题文件编辑器缺「新建文件」** — 只能编辑已有文件，无法通过后台创建新模板/CSS
- [x] **`theme.yaml` 字段可扩展** — 可加 `screenshot`、`license`、`tags` 等元数据

## 🟢 低优先级

- [x] **静态资源缺缓存头** — 已实现：`/theme-assets/` 1年缓存（带版本号），`/assets/`、`/wp-content/` 1天缓存
- [x] **图片无缩略图** — 已实现：上传时自动生成 150w/300w/768w 缩略图，文章渲染时自动添加 srcset+sizes+lazy loading
- [x] **评论缺验证码** — 已有蜜罐+限频+邮箱校验，个人博客够用，不加验证码避免降低体验
- [x] **缺 RSS 自动发现标签** — single 主题已有 `<link rel="alternate">`
- [x] **缺暗色模式切换** — default 主题已有 8 种外观（暗黑/明亮/红梅/绿竹/藕粉/橙黄/蔚蓝/韵紫）
- [x] **缺数据库备份机制** — 已实现：手动备份、自动每日备份（凌晨3点）、邮件附件发送、恢复（含SQLite魔数校验+紧急备份）
- [x] **缺定时发布** — 已实现：`StatusScheduled` 状态 + `PublishedAt` 未来时间 + goroutine 每分钟自动发布
- [x] **清理已合并的本地分支** — 已清理，仅剩 feat/theme 和 main
