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
   grep "<trace_id>" data/blog.log | grep "sql details"
   ```
   日志中每条 SQL 都带有序号和 hint，可直接定位来源
3. **按 hint 分组统计**，找出重复查询和可优化点
4. **后台开启 SQL 调试**：设置页开启后，管理员登录时页面底部直接显示 SQL 详情

## 2. SQL 查询现状

### 2.1 首页（`/`）当前 26 条 SQL

实测日志（trace_id: `4f288631928f1031`）：

| # | Hint | 说明 |
|---|---|---|
| 01 | `syncPostPermalink -> GetSettings` | 同步固定链接配置（3 个 setting） |
| 02 | `loadSettings -> GetSettings` | 加载站点设置（8 个 setting） |
| 03 | `ListPosts -> COUNT(*)` | 文章总数 |
| 04 | `ListPosts -> users` | GORM Preload("Author") |
| 05 | `ListPosts -> post_categories` | GORM Preload("Categories") 中间表 |
| 06 | `ListPosts -> categories` | GORM Preload("Categories") |
| 07 | `ListPosts -> post_tags` | GORM Preload("Tags") 中间表 |
| 08 | `ListPosts -> tags` | GORM Preload("Tags") |
| 09 | `ListPosts -> posts` | 文章列表主查询 |
| 10 | `ApprovedCommentCounts` | 批量查评论数 |
| 11 | `theme.Manager.Current -> GetSetting` | 第 1 次查 current_theme（pageConfig） |
| 12 | `MenuPages` | 导航菜单页面 |
| 13 | `RecentComments` | 近期评论（8 条） |
| 14-17 | `PostMetas` + Preloads | RecentComments 关联查询（作者/分类/文章） |
| 18-19 | `SayingComments` | 博主动态评论 |
| 20-22 | `PostMetas` + Preloads | SayingComments 关联查询 |
| 23 | `ArchiveMonths` | 归档月份统计 |
| 24 | `AllCategories` | 全部分类 |
| 25 | `AllTags` | 全部标签 |
| 26 | `theme.Manager.Current -> GetSetting` | 第 2 次查 current_theme（base 内 currentThemeName） |

### 2.2 问题归类

| 问题 | 涉及 SQL | 根因 |
|---|---|---|
| GORM Preload 碎片查询 | #04-08, #14-17, #20-22 | 每个关联都单独一条 SQL，N+1 问题 |
| current_theme 重复查询 | #11, #26 | `pageConfig()` 和 `base()` 各调一次 `Current()` |
| 关联数据跨请求不共享 | #13-17 vs #18-22 | 同一批 posts 被不同 widget 重复查 PostMetas |

## 3. 优化方向：全量数据预加载

### 3.1 核心思路

```
之前: handler → store.XXX(Preload) → GORM 自动查关联 → N 条 SQL
之后: handler → loader.LoadAll() → store.XXX(无Preload) → loader.Fill() → ~8 条 SQL
```

**原则**：
- store 层只返回表对应的 model，不做 Preload
- 上层需要关联数据时，从 DataLoader 的内存 map 中填充
- 每次请求开始时，全量加载所有表到内存

### 3.2 数据规模评估

当前数据量（从日志和 DB 文件推断）：

| 表 | 行数 | 大小估算 |
|---|---|---|
| posts | ~2000 | ~1.5 MB |
| comments | ~1000 | ~0.3 MB |
| categories | 11 | <1 KB |
| tags | 200 | ~10 KB |
| users | 1-2 | <1 KB |
| post_categories | ~50 | <1 KB |
| post_tags | ~200 | ~10 KB |
| settings | ~12 | <1 KB |
| **合计** | **~3500** | **~2 MB** |

### 3.3 增长预估与可行性

假设：1 天 1 篇文章，每篇 5 条评论（实际远不会这么多）

| 时间 | 文章数 | 评论数 | DB 大小 | 全量加载耗时 |
|---|---|---|---|---|
| 现在 | ~2000 | ~1000 | 2.4 MB | ~2ms |
| 1 年 | ~2365 | ~2825 | ~3 MB | ~3ms |
| 5 年 | ~3825 | ~10K | ~5 MB | ~5ms |
| 10 年 | ~5650 | ~19K | ~8 MB | ~8ms |
| 30 年 | ~13K | ~56K | ~20 MB | ~20ms |

**结论**：按个人博客的写作速度，全量加载方案可流畅运行 30 年以上。1C2G 机器完全够用。

### 3.4 效果预估

| 页面 | 当前 SQL | 优化后 SQL | 减少 |
|---|---|---|---|
| 首页 | 26 | ~8 | -69% |
| 文章页 | 37 | ~10 | -73% |
| 页面 | ~20 | ~10 | -50% |

## 4. 缓存优化具体方案

### 4.1 DataLoader 设计

```go
// internal/store/loader.go

type DataLoader struct {
    Posts       map[uint]*model.Post       // 全部已发布文章+页面
    Comments    map[uint]*model.Comment    // 全部已批准评论
    Categories  map[uint]*model.Category
    Tags        map[uint]*model.Tag
    Users       map[uint]*model.User
    Settings    map[string]string

    // 预计算的关联索引
    postCategories map[uint][]uint  // postID → categoryIDs
    postTags       map[uint][]uint  // postID → tagIDs
    commentsByPost map[uint][]uint  // postID → commentIDs
}

func LoadAll(ctx context.Context, db *gorm.DB) (*DataLoader, error) {
    // 8 条 SELECT * 全表查询
    // 构建内存索引
}

func (l *DataLoader) FillPost(p *model.Post) {
    p.Author = l.Users[p.AuthorID]
    p.Categories = l.categoriesForPost(p.ID)
    p.Tags = l.tagsForPost(p.ID)
    p.CommentCount = len(l.commentsByPost[p.ID])
}
```

### 4.2 Store 层改造

去掉所有 `.Preload("Categories").Preload("Tags").Preload("Author")`：

```go
// 之前
func (s *Store) ListPosts(ctx context.Context, ...) (*ListPostsResult, error) {
    q.Preload("Categories").Preload("Tags").Preload("Author").
        Order("published_at DESC").Find(&posts)
}

// 之后
func (s *Store) ListPosts(ctx context.Context, ...) (*ListPostsResult, error) {
    q.Order("published_at DESC").Find(&posts)
    // 关联填充由上层 DataLoader.FillPost() 完成
}
```

### 4.3 Handler 层改造

```go
func (h *Public) Index(c *gin.Context) {
    loader, _ := store.LoadAll(c, h.st.DB())  // 8 条 SQL

    s := h.loadSettingsFromLoader(loader)       // 0 条 SQL（从内存读）
    res := h.st.ListPosts(c, page, s.PageSize, "", "")  // 2 条 SQL（COUNT + 主查询）
    for i := range res.Posts {
        loader.FillPost(&res.Posts[i])          // 内存填充
    }

    // base() 中所有数据从 loader 取，不再查 DB
    data := h.baseFromLoader(c, loader, ...)
}
```

### 4.4 实施步骤

1. **Phase 1**：创建 `DataLoader`，实现全量加载 + 索引构建
2. **Phase 2**：改造 `Index()` 使用 DataLoader，验证效果
3. **Phase 3**：逐步改造 `Post()`、`Page()`、`Category()`、`Tag()` 等
4. **Phase 4**：清理 store 层所有 Preload 调用
5. **Phase 5**：考虑全局缓存（写操作时失效），避免每次请求都全量加载

### 4.5 注意事项

- **写操作一致性**：写操作（发布文章、审核评论等）后需刷新缓存。初期可每次请求全量加载（简单可靠），后续可改为全局缓存 + 写失效
- **Content 字段**：`Content` 和 `ContentMD` 是 TEXT 大字段，列表页不需要时可只加载必要列
- **后台管理**：后台页面数据量小，可同样受益；但导入/导出等批量操作应保持直接 DB 查询
- **测试**：改造后需确保 `go test ./...` 通过，store 层测试可能需要适配
