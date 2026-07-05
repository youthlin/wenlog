# SQL 查询优化方案

## 1. GORM Plugin：SQL 追踪与 Hint 注入

### 1.1 架构

```
请求进入 → SQLTracer 中间件 → ctx 注入 SQLDetails 收集器
                                    ↓
         handler → store.db(ctx) → GORM 执行 SQL
                                    ↓
         GormSQLTracer 插件: before(记录开始时间) → injectSQLHint → after(记录耗时/详情)
                                    ↓
         请求结束 → SQLTracer 中间件 → 日志输出 SQL 详情
```

### 1.2 关键文件

| 文件 | 职责 |
|---|---|
| `internal/store/gorm_plugin.go` | GORM 插件：SQL hint 注入、耗时统计、Prometheus 打点 |
| `internal/middleware/sql_tracer.go` | Gin 中间件：请求级 SQLDetails 收集与日志输出 |
| `internal/store/store.go:50` | `db(ctx)` 将 ctx 传入 GORM，确保插件能拿到 ctx |

### 1.3 SQL Hint 机制

每条 SQL 执行前，`injectSQLHint` 回调会自动在 SQL 语句中注入 `/*caller -> callee*/` 注释：

```sql
SELECT /*handler.(*Public).Index -> store.(*Store).ListPosts*/ count(*) FROM `posts` ...
```

**实现原理**（`gorm_plugin.go:115-156`）：
- 使用 `runtime.Callers()` 获取调用栈
- 第一遍扫描：找到 store 层方法（callee）
- 第二遍扫描：跳过 store 包内部帧，找到上层调用者（caller）
- 格式化为 `caller -> callee`

也可手动设置 hint：
```go
ctx = store.CtxWithSQLHint(ctx, "custom hint")
```

### 1.4 SQL 分析方法

1. **浏览器访问页面**，从响应头获取 `X-Trace-Id`
2. **从日志 grep SQL**：
   ```bash
   grep "<trace_id>" data/wenlog.log | grep "sql details"
   ```
   日志中每条 SQL 都带有序号和 hint，可直接定位来源
3. **按 hint 分组统计**，找出重复查询和可优化点
4. **后台开启 SQL 调试**：设置页开启后，管理员登录时页面底部直接显示 SQL 详情

## 2. 最终效果

### 2.1 缓存命中后 SQL 数量

| 页面 | 匿名访客 | 登录用户 | 说明 |
|------|----------|----------|------|
| 首页 | **0** | **0** | 全部从 DataLoader 内存读取 |
| 文章/页面详情 | **1** | **1** | 仅 `UPDATE views=views+1` 写入 |
| 分类/标签列表 | **0** | **0** | 全部从 DataLoader 内存读取 |

### 2.2 首次请求（缓存冷启动）

首次请求执行 8 条 `SELECT *` 全量加载（`LoadAll`），后续请求全部命中缓存：

| # | 查询 | 说明 |
|---|---|---|
| 01 | `SELECT * FROM posts WHERE status = 'published'` | 全部已发布文章+页面 |
| 02 | `SELECT * FROM comments WHERE status IN ('approved','pending')` | 全部已批准+待审评论 |
| 03 | `SELECT * FROM categories` | 全部分类 |
| 04 | `SELECT * FROM tags` | 全部标签 |
| 05 | `SELECT * FROM users` | 全部用户 |
| 06 | `SELECT * FROM settings` | 全部设置 |
| 07 | `SELECT * FROM post_categories` | 文章-分类关联 |
| 08 | `SELECT * FROM post_tags` | 文章-标签关联 |

### 2.3 唯一无法消除的 SQL

`UPDATE posts SET views=views+1 WHERE id = ?` — 浏览量写入，这是必须的写操作。

### 2.4 后台管理页面

后台保持直接查 DB，不上 DataLoader 缓存。原因：
- 后台是写密集型（CRUD），每次写入都会触发 `InvalidateCache()`，缓存命中率极低
- 流量极低（仅管理员一人），SQLite 单表几万行内亚毫秒级响应
- 后台需要即时反映最新数据，缓存反而增加复杂度

## 3. 优化方案：全量数据预加载 + 全局缓存

### 3.1 核心思路

```
之前: handler → store.XXX(Preload) → GORM 自动查关联 → N 条 SQL
之后: handler → LoadAllCached() → 8 条 SQL（首次）→ 0 条 SQL（缓存命中）
```

**原则**：
- 请求开始时全量加载 8 张表到内存，构建索引
- 后续所有查询走内存 map/索引，不再查 DB
- 全局缓存（`sync.RWMutex` + 双检锁），首次加载后所有请求复用
- 写操作（INSERT/UPDATE/DELETE）后调用 `InvalidateCache()` 清缓存

### 3.2 数据规模评估

当前数据量：

| 表 | 行数 | 大小估算 |
|---|---|---|
| posts | ~93 | ~1.5 MB |
| comments | ~1538 | ~0.5 MB |
| categories | 11 | <1 KB |
| tags | 200 | ~10 KB |
| users | 2 | <1 KB |
| post_categories | ~92 | <1 KB |
| post_tags | ~239 | ~10 KB |
| settings | 20 | <1 KB |
| **合计** | **~2200** | **~2 MB** |

### 3.3 增长预估与可行性

假设：1 天 1 篇文章，每篇 5 条评论

| 时间 | 文章数 | 评论数 | DB 大小 | 全量加载耗时 |
|---|---|---|---|---|
| 现在 | ~93 | ~1538 | ~2 MB | ~30ms |
| 1 年 | ~458 | ~3363 | ~3 MB | ~40ms |
| 5 年 | ~1918 | ~10K | ~5 MB | ~50ms |
| 10 年 | ~3743 | ~19K | ~8 MB | ~80ms |
| 30 年 | ~11K | ~56K | ~20 MB | ~200ms |

**结论**：按个人博客的写作速度，全量加载方案可流畅运行 30 年以上。

### 3.4 实测效果

| 页面 | 优化前 SQL | 首次请求 | 缓存命中 | 减少 |
|------|-----------|----------|----------|------|
| 首页 | 26 | 8 | **0** | -100% |
| 文章页 | 37 | 9 | **1** | -97% |
| 页面 | ~20 | 9 | **1** | -95% |
| 分类/标签 | ~15 | 8 | **0** | -100% |

> 文章/页面缓存命中后仅 1 条 SQL：`UPDATE posts SET views=views+1`（浏览量写入，无法消除）。

## 4. DataLoader 设计

### 4.1 数据结构

```go
// internal/store/loader.go

type DataLoader struct {
    Posts      map[uint]*model.Post
    Comments   map[uint]*model.Comment
    Categories map[uint]*model.Category
    Tags       map[uint]*model.Tag
    Users      map[uint]*model.User
    Settings   map[string]string

    // 预计算索引
    postCategories    map[uint][]uint // postID → categoryIDs
    postTags          map[uint][]uint // postID → tagIDs
    commentsByPost    map[uint][]uint // postID → commentIDs (approved only, 公开)
    allCommentsByPost map[uint][]uint // postID → commentIDs (approved + pending)
    postsBySlug       map[string]*model.Post
    postsByType       map[string][]*model.Post
    menuPages         []*model.Post
    archiveMonths     []ArchiveMonth
}
```

### 4.2 核心方法

| 方法 | 说明 |
|------|------|
| `LoadAll(ctx)` | 8 条 SQL 全量加载，构建所有索引 |
| `LoadAllCached(ctx)` | 全局缓存版，`sync.RWMutex` + 双检锁 |
| `InvalidateCache()` | 写操作后清缓存，下次请求自动重建 |
| `FillPost(p)` | 从内存填充 Author、Categories、Tags、CommentCount |
| `ListPosts(page, pageSize, catSlug, tagSlug)` | 纯内存分页+过滤 |
| `CommentPage(postID, page, pageSize, currentUserID)` | 纯内存评论分页，登录用户可见自己的 pending |
| `ResolvePostByPath(path, match)` | 从内存解析文章路径 |
| `GetPageBySlug(slug)` | 从内存查页面 |
| `RecentPosts(n)`, `RecentComments(n)`, `SayingComments(...)` | Widget 数据 |
| `AllCategories()`, `AllTags()`, `ArchiveMonths()`, `MenuPages()` | 侧边栏数据 |
| `GetSetting(key)`, `GetSettings(keys)` | 设置项读取 |
| `PostMeta(id)`, `PostMetas(ids)`, `PrevPost(p)`, `NextPost(p)` | 文章导航 |
| `CommentWidgetItems(comments)` | 评论 Widget 数据组装 |

### 4.3 全局缓存机制

```go
// store.go
type Store struct {
    cacheMu sync.RWMutex
    cache   *DataLoader
}

func (s *Store) LoadAllCached(ctx context.Context) (*DataLoader, error) {
    // 读锁快速路径
    s.cacheMu.RLock()
    if s.cache != nil {
        cacheHits.Inc()
        s.cacheMu.RUnlock()
        return s.cache, nil
    }
    s.cacheMu.RUnlock()

    // 写锁 + 双检
    s.cacheMu.Lock()
    defer s.cacheMu.Unlock()
    if s.cache != nil {
        cacheHits.Inc()
        return s.cache, nil
    }
    cacheMisses.Inc()
    l, err := s.LoadAll(ctx)
    if err != nil {
        return nil, err
    }
    s.cache = l
    return l, nil
}

func (s *Store) InvalidateCache() {
    s.cacheMu.Lock()
    s.cache = nil
    s.cacheMu.Unlock()
}
```

### 4.4 Prometheus 指标

| 指标 | 说明 |
|------|------|
| `wenlog_sql_total` | SQL 执行总数（按 sql_type + error 分组） |
| `wenlog_sql_duration_seconds` | SQL 执行耗时分布 |
| `wenlog_dataloader_cache_hits_total` | 缓存命中次数 |
| `wenlog_dataloader_cache_misses_total` | 缓存未命中次数 |

### 4.5 Handler 层改造

`DynamicOrLegacy` 入口处统一加载 DataLoader，所有子 handler 复用：

```go
func (h *Public) DynamicOrLegacy(c *gin.Context) {
    loader, err := h.st.LoadAllCached(c)  // 首次 8 SQL，后续 0
    // ...
    if slug, ok := singleSegmentSlug(path); ok {
        if h.pageExistsFromLoader(loader, slug) {
            h.pageWithLoader(c, loader)     // 0 SQL
        }
    }
    if match, ok := permalink.ParsePostPath(path); ok {
        h.renderResolvedPostWithLoader(c, path, match, loader)  // 1 SQL (views)
    }
}
```

`base()` 在 loader 非 nil 时全部走内存：

```go
func (h *Public) base(c *gin.Context, ..., loader *store.DataLoader) gin.H {
    if loader != nil {
        menu = loader.MenuPages()           // 内存
        currentUser = currentUserFromLoader(c, loader)  // 内存
    }
}
```

### 4.6 评论可见性

| 场景 | 数据来源 | SQL |
|------|----------|-----|
| 匿名访客 | `commentsByPost`（仅 approved） | 0 |
| 登录用户 | `allCommentsByPost`（approved + 自己的 pending） | 0 |
| 匿名+待审评论 | `VisibleCommentsPageForViewer`（DB） | 4-8 |

> 第三种场景极少见（匿名用户刚提交评论后刷新页面），保留 DB 查询作为兜底。

### 4.7 实施步骤

1. **Phase 1** ✅：创建 `DataLoader`，实现全量加载 + 索引构建
2. **Phase 2** ✅：改造 `Index()` 使用 DataLoader
3. **Phase 3** ✅：改造 `Post()`、`Page()`、`Archive()`、`Category()`、`Tag()` 使用 DataLoader
4. **Phase 4** ✅：清理 store 层 Preload 调用
5. **Phase 5** ✅：全局缓存（`LoadAllCached` + `InvalidateCache`）
6. **Phase 6** ✅：`syncPostPermalink` 和 `current_theme` 改为从 DataLoader 内存读取 → 首页 0 SQL
7. **Phase 7** ✅：`ResolvePostByPath` 改为 DataLoader 内存解析 → 文章页 1 SQL
8. **Phase 8** ✅：`CommentPage` 从内存分页评论 → 匿名访客评论 0 SQL
9. **Phase 9** ✅：登录用户评论走内存 + `currentUser` 从 DataLoader 取 → 登录态 1 SQL

### 4.8 注意事项

- **写操作一致性**：所有 INSERT/UPDATE/DELETE 后调用 `InvalidateCache()`，下次请求自动重建
- **Content 字段**：`Content` 和 `ContentMD` 是 TEXT 大字段，全量加载时一并加载（数据量小，影响可忽略）
- **后台管理**：不上 DataLoader，保持直接 DB 查询（写密集、低流量、需即时数据）
- **Search**：搜索需要 LIKE 查询，保持直接 DB 查询（`loader = nil` 回退）
