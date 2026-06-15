# 项目全面 Review 报告

> 生成日期: 2026-06-15 | 分支: feat/markdown-editor-ux

## 一、安全问题 (Security)

### 🔴 高危

| # | 状态 | 问题 | 位置 | 说明 |
|---|------|------|------|------|
| 1 | ✅ 已修复 | **评论提交无 CSRF 保护** | `cmd/server/routes.go:30`, `internal/handler/comment.go:55` | `POST /comment` 是状态变更端点但未应用 CSRF 中间件。登录用户评论时自动关联身份，攻击者可伪造请求以该用户身份发评论。 |
| 2 | ✅ 已修复 | **登录接口无限速** | `cmd/server/routes.go:46` | 已添加基于内存的 IP 频率限制中间件，15 分钟内最多 5 次。 |
| 3 | ✅ 已修复 | **忘记密码接口无限速** | `cmd/server/routes.go:35` | 已添加基于内存的 IP 频率限制中间件，15 分钟内最多 5 次。 |
| 4 | ✅ 已修复 | **Debug SQL 可执行多语句** | `internal/handler/admin.go:1648-1652` | `allowDebugSQL` 只检查前缀是否为 `SELECT`/`EXPLAIN`，未阻止多语句注入（如 `SELECT 1; DROP TABLE users;--`）。 |
| 5 | ✅ 已修复 | **Session Cookie 缺少 Secure 标志** | `cmd/server/web.go:65-69`, `internal/middleware/session.go:59-70` | `Secure` 标志未在初始配置中设置，且动态设置存在竞态条件（共享 `sessionOption` 被持久修改）。部署在反向代理后可能永远不设 Secure。 |

### 🟡 中危

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 6 | ✅ 已修复 | **无密码强度要求** | `auth.go:120-164`, `admin.go:438-473, 1360-1393, 3025-3070` — 所有密码设置端点只检查非空，可设 1 字符密码 |
| 7 | ✅ 已修复 | **注册接口无限速** | `routes.go:48` | 已添加基于内存的 IP 频率限制中间件，1 小时内最多 3 次。 |
| 8 | ✅ 已修复 | **CSRF 同源检查在无 Origin/Referer 时放行** | `middleware/csrf.go:108-109` | 改为严格模式，无 Origin/Referer 头时拒绝请求。 |
| 9 | ✅ 已修复 | **Markdown 预览不消毒 HTML** | `admin.go:2576-2580, 3259-3265` — `gomarkdown` 默认允许原始 HTML 通过，可注入 `<script>` |

### 🟢 低危

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 10 | ✅ 已修复 | **Open Redirect** | `admin.go` 多处 | 添加 `safeRedirect` 函数校验 Referer 域名，非本站域名回退到 `/admin/comments`。 |
| 11 | ✅ 已知设计 | **SMTP/Metrics 密码明文存储** | `consts/const.go:31,38` | 自托管单用户博客，basic auth 和 SMTP 密码可随时更改撤销，风险可控。 |
| 12 | ✅ 已修复 | **密码变更无通知邮件** | `auth.go:119-164`, `admin.go:1360-1393` | 添加 `sendPasswordChangeNotification` 异步发送通知邮件。 |
| 13 | ✅ 已修复 | **评论 URL 未校验协议** | `comment.go:47,73` — 可提交 `javascript:` URL |
| 14 | ✅ 已修复 | **用户枚举时序侧信道** | `auth.go:42-49`, `admin.go:273-278` — 登录/忘记密码可通过响应时间判断用户是否存在 | 登录始终执行 bcrypt 比较，忘记密码添加固定延迟。 |
| 15 | ✅ 已修复 | **用户名无格式校验** | `admin.go:303-392, 3025-3070` — 可包含特殊字符、空格等 | 添加 `validateUsernameT` 校验用户名格式。 |

---

## 二、i18n 国际化

### 🔴 严重

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 16 | ✅ 已修复 | **邮件通知硬编码中文** | `internal/handler/comment.go:222-227` | `commentReplyMail` 改为接受 `*gettext.Translations` 参数，邮件标题和正文通过 `tr.T()` 翻译。 |

### 🟡 高

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 17 | ✅ 已修复 | **语言名称 "中文" 硬编码** | `admin_base.gohtml:98` — 应使用 `.t.T("中文")` |
| 18 | ✅ 已修复 | **语言名称 "中文" 硬编码** | `base.gohtml:29` — 应使用 `.t.T("中文")` |
| 19 | ✅ 已修复 | **语言名称 "中文" 硬编码** | `admin_login.gohtml:16` — 应使用 `.t.T("中文")` |
| 20 | ✅ 已修复 | **语言名称 "中文" 硬编码** | `admin_register.gohtml:16` — 应使用 `.t.T("中文")` |

### 🟡 中

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 21 | ✅ 已修复 | **en_US.po 有 12 条空翻译** | `web/i18n/en_US.po` — 包括 `%s・后台`、`后台导航`、`界面设置`、`颜色`、`查询`、固定链接校验错误、`分类管理` 等 |
| 22 | ✅ 已修复 | **en_US.po 有 10 条 fuzzy 翻译** | `概览` 译成 "Views"（应为 "Overview"）、`标签管理` 译成 "Categories / Tags"、`文章 slug` 相关错误译成 "Page"/"Tag" 等 |

### 🟢 低

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 23 | ✅ 已修复 | **`%d 条待审` 英文单复数相同** | `en_US.po:303-306` — `msgstr[0]` 和 `msgstr[1]` 都是 `"%d pending"`，英文应有单复数区分 |

---

## 三、代码质量

### 过长函数 (>100 行)

| # | 状态 | 函数 | 位置 | 行数 |
|---|------|------|------|------|
| 24 | ✅ 已修复 | `SavePost` | `admin.go:2336-2476` | ~140 行 → 拆分为 `resolvePostForSave`、`postEditError`、`validateAndCheckPageSlug`、`validateAndCheckPostSlug` |
| 25 | ✅ 已修复 | `SubmitComment` | `comment.go:55-167` | ~112 行 — 拆分为 `validateCommentInput`、`validateCommentTarget`、`checkCommentRateLimit`。 |
| 26 | ✅ 已修复 | `SaveSiteSettings` | `admin.go:991-1096` | ~105 行 → 拆分为 `validateSiteSettingsPermalink`、`setSettings` |
| 27 | ✅ 已修复 | `SaveProfileSettings` | `admin.go:1225-1328` | ~103 行 → 拆分为 `profileError`、`handleEmailChange` |
| 28 | ✅ 已修复 | `termsDataForSection` | `admin.go:2009-2113` | ~105 行 → 拆分为 `fillTagTermsData`、`fillCategoryTermsData`、`fillPagination` |

### 无用代码

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 29 | ✅ 已修复 | `settingsData` 定义但从未调用 | `admin.go:849` |
| 30 | ✅ 已修复 | `.widget-user-greeting` CSS 规则未在任何模板中使用 | `style.css:312` |
| 31 | ✅ 已修复 | `.avatar-picture` CSS 规则未在模板中使用 | `style.css:179` | 已移除。 |

### 代码重复

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 32 | ✅ 已修复 | `renderMarkdown` 和 `renderMarkdownForBootstrap` 完全重复 | `admin.go:3259` vs `bootstrap.go:193` | 统一到 `render.RenderMarkdown`。 |
| 33 | ✅ 已修复 | `firstNonEmpty` 三处重复实现 | `admin.go:1654`, `public.go:500`, `exporter.go:387` | 统一到 `util.FirstNonEmpty`。 |
| 34 | ✅ 已修复 | `normalizeDefaultAvatar` 两处重复 | `admin.go:961` vs `render.go:308` | 统一到 `util.NormalizeDefaultAvatar`。 |
| 35 | ✅ 已修复 | `pathExists` 两处重复 | `admin.go:1608` vs `i18n.go:74` | 统一到 `util.PathExists`。 |
| 36 | ✅ 已修复 | `normalizeTermSlug` 和 `slugifyTag` 功能相似 | `admin.go:2504` vs `store/admin.go:524` | 统一到 `util.Slugify`。 |
| 37 | ✅ 已修复 | X-Forwarded 头解析逻辑重复 | `request.go:14-36` vs `csrf.go:118-139` | 导出 `middleware.RequestScheme`/`RequestHost` 统一使用。 |
| 38 | ✅ 已修复 | `currentUser`/`currentUserID` 在 Admin 和 Public 中重复 | `admin.go:1661,2479` vs `public.go:157,170` | 提取为包级 `currentUserID`/`currentUserByStore` 共享函数。 |

### 错误处理缺失

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 39 | ✅ 已修复 | `strconv.ParseUint` 错误被忽略（12 处） | `admin.go` 多处 — ID 解析失败时 id=0，可能导致意外操作 | 新增 `parseUintParam` 辅助函数，解析失败返回 404。 |
| 40 | ✅ 已修复 | `GetSetting`/`GetSettings` 错误被忽略 | `admin.go:82-83,485,863,1131`, `public.go:101`, `auth.go:172,206` | 添加错误日志记录。 |
| 41 | ✅ 已修复 | `IncrementViews` 错误被忽略 | `public.go:264,316` | 添加错误日志记录。 |
| 42 | ✅ 已修复 | `ClearResetToken` 错误被忽略 | `auth.go:69,82,159` — 失败则 token 可被重用 | 添加错误日志记录。 |
| 43 | ✅ 已修复 | `RecentCommentCountByIP` 错误被忽略 | `comment.go:126` — 失败则频率限制被绕过 | 错误时拒绝评论并记录日志。 |
| 44 | ✅ 已修复 | Feed XML 编码错误被忽略 | `feed.go:69` — 客户端收到截断的 200 响应 | 添加错误日志记录。 |

### 魔法数字

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 45 | ✅ 已修复 | 大量硬编码数字应提取为常量 | token 长度 (`32`, `24`, `12`)、超时时间 (`24*time.Hour`, `1*time.Hour`)、上传大小 (`10<<20`)、session 有效期 (`7*86400`)、头像尺寸 (`48`)、评论摘要长度 (`36`)、`"uncategorized"` slug 等 | 新增 `AvatarSizeSmall`, `CommentSnippetMaxRune`, `SessionMaxAge`, `DefaultCategorySlug` 等常量。 |

---

## 四、角色权限

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 46 | ✅ 已修复 | **文档与实际文件清单不一致** | `docs/roles-and-permissions.md` 列出了 `profile.go`、`my_comments.go`、`account.go` 等独立文件，但实际这些功能都合并在 `admin.go` 中 | 更新文档反映实际文件结构。 |
| 47 | ✅ 已知设计 | **author 可查看所有文章列表** | `routes.go:76` — 文档已说明"当前可看到全部但只能编辑自己的"，是已知设计权衡 |
| 48 | ✅ 符合设计 | **subscriber 登录后台只能看到 Dashboard** | 侧边栏按角色过滤已实现，subscriber 仍可访问 `/admin/profile`、`/admin/my-comments` 等路由（这些在 profileGroup 中，所有角色可访问），符合设计 |

---

## 五、CSS / 响应式 / 无障碍

### 响应式

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 49 | ⬜ 待修复 | 缺少 860px-1080px 之间的中间断点 | `style.css:204-244` — 1024px 平板竖屏时侧边栏 280px 可能偏窄 |
| 50 | ✅ 已修复 | 登录框在 320px-640px 之间固定 320px 宽度 | `admin.css:1122` — 应使用 `min(320px, calc(100vw - 24px))` |

### 无障碍 (Accessibility)

| # | 状态 | 严重度 | 问题 | 位置 |
|---|------|--------|------|------|
| 51 | ⬜ 待修复 | 🔴 高 | **表单缺少 `<label>` 元素** | `admin_login.gohtml:37-38`, `post.gohtml:93-98`, `auth_forgot_password.gohtml:8` — 登录、评论、忘记密码表单只用 `placeholder` 当标签，不符合 WCAG SC 3.3.2 |
| 52 | ✅ 已修复 | 🔴 高 | **缺少全局 `:focus-visible` 样式** | `style.css`, `admin.css` — 键盘用户难以看到焦点位置，不符合 WCAG SC 2.4.7 |
| 53 | ✅ 已修复 | 🟡 中 | **浅色主题 muted 文字对比度不足** | `style.css:26` — `#8a94a0` 在 `#fafbfc` 上对比度约 2.8:1，不满足 AA 4.5:1 |
| 54 | ✅ 已修复 | 🟡 中 | **Badge 红色对比度不足** | `admin.css:332` — `#e74c3c` 在 `#fff` 上约 4.0:1，12px 字号不满足 AA |
| 55 | ✅ 已修复 | 🟡 中 | **缺少 Skip Navigation 链接** | `base.gohtml`, `admin_base.gohtml` — 不符合 WCAG SC 2.4.1 |
| 56 | ⬜ 待修复 | 🟡 中 | **自定义 select 缺少方向键导航** | `theme.js:31-184` — 有 `role="listbox"` 但无 `aria-activedescendant` 和方向键处理 |

### 浏览器兼容性

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 57 | ⬜ 待修复 | `color-mix()` 无 fallback | `style.css`, `admin.css` 大量使用 — 旧浏览器会丢失背景/边框色 |
| 58 | ⬜ 待修复 | `:has()` 在旧 Firefox (<121) 不支持 | `admin.css:961,1239-1243` — 头像选中样式和表格卡片布局会失效 |
| 59 | ✅ 已修复 | 只用了 `-webkit-appearance: none` 缺少标准属性 | `style.css:195`, `admin.css:195` — 实际已有 `appearance: none` |

### 其他 CSS 问题

| # | 状态 | 问题 | 位置 |
|---|------|------|------|
| 60 | ✅ 已修复 | 双分号 typo | `style.css:202` — `max-width: 100%;;` |
| 61 | ✅ 已修复 | 缺少 `@media print` 样式 | 完全没有打印样式，博客文章打印体验差 |
| 62 | ⬜ 待修复 | 按钮触摸目标偏小 | `admin.css:465-466` — `.btn` 高度约 32-34px，低于推荐 44px |
| 63 | ⬜ 待修复 | 硬编码颜色不随主题变化 | `admin.css` 多处 — 阴影、badge、banner 使用固定颜色 |
| 64 | ⬜ 待修复 | `!important` 使用过多 (14+ 处) | `admin.css` — 反映 CSS 架构需要重构 |

---

## 修复优先级

1. **第一优先级（安全 + 数据保护）**: #1, #2, #3, #4, #5, #6, #9
2. **第二优先级（i18n + 无障碍）**: #16, #17-20, #21, #22, #51, #52
3. **第三优先级（代码质量）**: #24-28, #29-31, #32-38, #39-44, #45
4. **第四优先级（体验优化）**: #49-50, #53-64
