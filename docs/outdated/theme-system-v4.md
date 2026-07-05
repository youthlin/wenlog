# 主题系统 v4 设计方案：可自定义侧边栏小组件

> 历史设计稿：当前实现以 `AGENTS.md`、`docs/template-data-reference.md` 和 `docs/theme-system-optimization-plan.md` 为准。

## 1. 动机

v3 中侧边栏是纯模板——主题作者在 `sidebar.gohtml` 里直接写 HTML + 模板标签。这很灵活，但**用户无法自定义**：主题提供了 5 个小组件，用户不能只显示其中 3 个，也不能调整顺序。

v4 目标：**保持模板驱动的简洁性，同时让用户能通过后台勾选/排序侧边栏组件**。

## 2. 核心设计

```
theme.yaml 声明可用组件 → 后台勾选排序 → Setting 表存 JSON → 模板按配置渲染
```

- **主题声明**：`theme.yaml` 中声明 `widget_areas`（区域）和 `widgets`（可用组件列表）
- **内置组件**：系统提供 5 个常用组件（recent_posts、categories、tag_cloud、search、recent_comments），自带默认模板和数据逻辑
- **主题覆盖**：主题在 `widgets/` 目录下放同名模板即可覆盖内置组件的样式
- **主题自定义组件**：在 `theme.yaml` 声明 + 提供模板 + 可选 `functions.goyaegi` 提供数据
- **用户配置**：Setting 表存 JSON 数组（如 `["recent_posts","categories","tag_cloud"]`），按序渲染
- **默认行为**：用户未配置时，显示主题声明的全部组件（按 yaml 声明顺序）
- **切换主题**：缺失组件静默跳过，后台灰显标注，不自动删除配置

## 3. theme.yaml 扩展

```yaml
name: "default"
version: "4.0"
description: "默认主题"
author: "youthlin"

# 新增：组件区域声明
widget_areas:
  sidebar:
    name: "侧边栏"
    description: "文章页侧边栏区域"
  footer:
    name: "页脚"
    description: "页面底部区域"

# 新增：可用组件列表
widgets:
  - id: search
    area: sidebar
  - id: recent_posts
    area: sidebar
  - id: categories
    area: sidebar
  - id: tag_cloud
    area: sidebar
  - id: recent_comments
    area: sidebar
```

- `widget_areas`：声明主题有哪些可配置区域
- `widgets`：声明每个区域有哪些可用组件，`id` 对应内置组件名或主题自定义组件名
- 主题自定义组件需在 `widgets/<id>.gohtml` 提供模板

## 4. 内置组件

系统提供 5 个内置组件，数据逻辑由 Go 代码实现，默认模板在 `web/widgets/` 下 embed：

| ID | 名称 | 数据来源 | 默认模板 |
|----|------|----------|----------|
| `search` | 搜索框 | 无（纯 HTML） | `web/widgets/search.gohtml` |
| `recent_posts` | 最新文章 | `.RecentPosts` | `web/widgets/recent_posts.gohtml` |
| `categories` | 分类目录 | `.Categories` | `web/widgets/categories.gohtml` |
| `tag_cloud` | 标签云 | `.Tags` | `web/widgets/tag_cloud.gohtml` |
| `recent_comments` | 最近评论 | `.RecentCommentItems` | `web/widgets/recent_comments.gohtml` |

主题覆盖内置组件模板：在主题 `widgets/` 目录下放同名文件即可（如 `themes/default/widgets/recent_posts.gohtml`）。

## 5. 主题自定义组件

主题可在 `theme.yaml` 中声明非内置 ID 的组件：

```yaml
widgets:
  - id: popular_posts
    area: sidebar
```

需提供：
- `widgets/popular_posts.gohtml` — 模板
- 可选 `functions.goyaegi` 中注册主题函数提供数据

模板中通过 `themeInvoke` 获取自定义数据：
```gohtml
{{define "widget_popular_posts"}}
<section>
  <h3>热门文章</h3>
  <ul>{{range themeInvoke "popular_posts" "n" 5}}<li>...</li>{{end}}</ul>
</section>
{{end}}
```

## 6. 存储

Setting 表，每个区域一条记录：

```
Key:   widget_sidebar
Value: ["recent_posts","categories","tag_cloud"]
```

JSON 数组，按序排列。用户未配置时 key 不存在，回退到主题默认。

## 7. 模板渲染

新增模板函数 `themeWidgets`：

```gohtml
{{range themeWidgets "sidebar"}}
  {{template .TemplateName .}}
{{end}}
```

`themeWidgets` 逻辑：
1. 读取 Setting `widget_<area>`，解析 JSON 数组
2. 若未配置，使用主题 yaml 中该 area 的全部组件（按声明顺序）
3. 对每个组件 ID，查找模板：主题 `widgets/<id>.gohtml` → 内置 `web/widgets/<id>.gohtml`
4. 若模板不存在（切换主题后缺失），跳过该组件
5. 返回组件列表（含模板名），模板层按序渲染

## 8. 后台配置页

路径：`/admin/widgets`

功能：
- 按区域分组显示
- 每个区域列出可用组件（来自当前主题 yaml），勾选启用 + 拖拽排序
- 缺失组件（配置中有但当前主题未声明）灰显标注「当前主题不可用」
- 保存后写入 Setting 表

权限：Admin 可访问（widget 配置影响全站外观）。

## 9. 切换主题行为

| 场景 | 行为 |
|------|------|
| 新主题有同名组件 | 正常渲染，使用新主题模板 |
| 新主题缺少某组件 | 渲染时跳过，后台灰显 |
| 新主题有全新组件 | 默认不启用，用户需手动勾选 |
| 切回旧主题 | 配置完整恢复（Setting 未变） |

## 10. 与 v3 的对比

| 方面 | v3 | v4 |
|------|----|----|
| 侧边栏 | 纯模板，用户不可改 | 后台可勾选/排序 |
| 组件复用 | 每个主题自己写 | 内置 5 个通用组件 |
| 主题开发 | 写 sidebar.gohtml | 声明 yaml + 可选覆盖模板 |
| 向后兼容 | — | 未配置时行为与 v3 一致 |
| 新增概念 | 无 | widget_areas、widgets（yaml 字段） |

## 11. 实现计划

1. `model/model.go` — 无需新增模型（复用 Setting 表）
2. `internal/theme/theme.go` — Theme 结构体新增 `WidgetAreas`、`Widgets` 字段
3. `web/widgets/` — 新建 5 个内置组件默认模板
4. `internal/render/render.go` — 新增 `themeWidgets` 模板函数
5. `internal/handler/admin_widgets.go` — 新建后台配置 handler
6. `web/templates/admin_widgets.gohtml` — 新建后台配置模板
7. `cmd/server/routes.go` — 注册路由
8. `web/themes/default/theme.yaml` — 添加 widget_areas + widgets 声明
9. `web/themes/default/templates/sidebar.gohtml` — 改用 `themeWidgets` 渲染
