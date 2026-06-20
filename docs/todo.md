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

- [ ] **静态资源缺缓存头** — CSS/JS 每次重新请求
- [ ] **图片无缩略图** — 大图直接展示影响加载速度
- [ ] **评论缺验证码** — 仅有蜜罐+限频，无验证码保护
- [ ] **缺 RSS 自动发现标签** — header 未输出 `<link rel="alternate">`（默认主题已有，single 主题缺）
- [ ] **缺暗色模式切换** — default 主题无 `prefers-color-scheme` 支持
- [ ] **缺数据库备份机制** — SQLite 单文件无自动备份
- [ ] **缺定时发布** — Post 模型无 `ScheduledAt` 字段
- [ ] **清理已合并的本地分支** — 18 个本地分支，很多已合并
