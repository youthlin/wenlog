# 用户与权限系统技术方案

## 1. 背景

本项目是一个 Go 单体博客应用，此前用户模型扁平（无角色字段），认证是二元的：登录即拥有全部后台权限。随着需求演进，需要支持多角色协作场景：

- **博主（admin）**：站点所有者，全部权限
- **协作者（author）**：如情侣博客中的另一半，能管理自己的内容但不能改站点设置
- **读者（subscriber）**：注册用户，可登录评论、管理个人资料、导出/注销

## 2. 角色设计

参考 [WordPress Roles and Capabilities](https://wordpress.org/documentation/article/roles-and-capabilities/)，结合个人博客实际场景，采用 **3 角色枚举** 方案（非 WP 的 capability-based 体系）：

| 能力 | admin | author | subscriber |
|---|---|---|---|
| 站点设置 | ✅ | ❌ | ❌ |
| 用户管理 | ✅ | ❌ | ❌ |
| 导入/导出 | ✅ | ❌ | ❌ |
| Debug 控制台 | ✅ | ❌ | ❌ |
| 所有文章/页面 CRUD | ✅ | ❌ | ❌ |
| 自己的文章/页面 CRUD | ✅ | ✅ | ❌ |
| 上传文件 | ✅ | ✅ | ❌ |
| 管理所有评论 | ✅ | ❌ | ❌ |
| 管理自己文章下的评论 | ✅ | ✅ | ❌ |
| 分类/标签（创建/编辑/删除） | ✅ | ❌ | ❌ |
| 分类/标签（查看+分配） | ✅ | ✅ | ❌ |
| 登录评论 | ✅ | ✅ | ✅ |
| 个人资料管理 | ✅ | ✅ | ✅ |
| 我的评论管理 | ✅ | ✅ | ✅ |
| 数据导出（GDPR） | ✅ | ✅ | ✅ |
| 账号注销 | ✅ | ✅ | ✅ |

### 设计原则

- **简单枚举 > capability 矩阵**：个人博客不需要 WP 那种可动态组合的 capability 系统，3 个角色足够覆盖所有场景
- **向后兼容**：现有唯一用户（admin）启动时自动升级为 admin 角色，零迁移成本
- **session 存 role**：登录时将 role 写入 session，中间件检查无需每次查库

## 3. 技术架构

### 3.1 数据模型

```go
// User 模型新增 Role 字段
type User struct {
    // ... 原有字段 ...
    Role string `gorm:"size:16;default:subscriber"`
}

// Comment 模型新增 UserID（可空外键）
type Comment struct {
    // ... 原有字段 ...
    UserID *uint `gorm:"index"` // nil = 匿名评论
}

// 角色常量
const (
    RoleAdmin      = "admin"
    RoleAuthor     = "author"
    RoleSubscriber = "subscriber"
)
```

### 3.2 认证与授权流程

```
请求 → AuthRequired（检查登录态）
     → RequireRole("admin") 或 RequireRole("admin","author")
     → Handler
```

- `AuthRequired`：检查 session 中是否有 `uid`，无则跳转登录页
- `RequireRole(roles...)`：检查 session 中的 `role` 是否在允许列表中，不匹配返回 403
- 登录时 `SetSessionUser(c, userID, role)` 将 uid + role 写入 session
- 登出时 `ClearSession(c)` 清除 session

### 3.3 路由分组

```
/admin/* (AuthRequired + CSRF)
  ├── /admin/, /admin/logout                    → 所有角色
  ├── /admin/posts, /admin/comments, ...        → admin + author
  └── /admin/settings, /admin/users, ...        → admin only

前台
  ├── /register, /login, /logout                → 无需登录
  ├── /profile, /my-comments, /export-data, ... → 需登录（所有角色）
  └── /, /search, /feed, /comment, ...          → 公开
```

### 3.4 关键实现细节

**评论与用户关联**：
- 登录用户评论时自动写入 `Comment.UserID`
- 匿名评论 `UserID` 为 nil，完全向后兼容
- 用户注销时评论匿名化保留（`UserID` 设为 nil），不删除评论内容

**现有用户升级**：
- `ensureInitialAdmin()` 启动时调用 `EnsureAdminRole("admin")`
- 无论新建还是已有数据库，admin 用户始终拥有 admin 角色

**后台侧边栏按角色显示**：
- subscriber 登录后台只能看到 Dashboard
- author 看到「概览」+「内容」分组
- admin 看到全部菜单（含设置、工具、用户管理）

## 4. 文件清单

### 新增文件

| 文件 | 用途 |
|---|---|
| `internal/handler/auth.go` | 前台注册/登录/登出 handler |
| `internal/handler/profile.go` | 个人资料编辑/密码修改 handler |
| `internal/handler/my_comments.go` | 我的评论列表/删除 handler |
| `internal/handler/account.go` | 数据导出/账号注销 handler |
| `web/templates/auth_register.gohtml` | 注册页 |
| `web/templates/auth_login.gohtml` | 登录页 |
| `web/templates/profile.gohtml` | 个人资料页 |
| `web/templates/my_comments.gohtml` | 我的评论页 |
| `web/templates/account_delete.gohtml` | 注销确认页 |
| `web/templates/admin_users.gohtml` | 后台用户管理页 |

### 修改文件

| 文件 | 改动 |
|---|---|
| `internal/model/model.go` | User.Role, Comment.UserID, 角色常量 |
| `internal/store/admin.go` | 用户管理/评论按用户查询/数据导出方法 |
| `internal/middleware/auth.go` | RequireRole, SetSessionUser, ClearSession |
| `cmd/server/routes.go` | 路由按角色分组，新增前台用户路由 |
| `cmd/server/bootstrap.go` | 启动时确保 admin 角色 |
| `internal/handler/admin.go` | 登录用新 session API，用户管理 handler，base 数据加 role |
| `internal/handler/comment.go` | 登录用户评论写入 UserID |
| `web/templates/admin_base.gohtml` | 侧边栏按角色显示菜单 |
| `web/templates/base.gohtml` | 导航栏加登录/注册/个人资料入口 |

## 5. 后续可扩展方向

- **注册审核**：当前注册即生效，可加邮箱验证或管理员审核
- **OAuth 登录**：GitHub/Google 等第三方登录
- **内容审核流**：author 的文章需 admin 审核后才能发布
- **更细粒度的内容权限**：author 只能看自己的文章列表（当前可看到全部但只能编辑自己的）
