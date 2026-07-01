# 主题系统 v6 设计方案：组件级选项 + 可重复组件

> 历史设计稿：当前实现以 `AGENTS.md`、`docs/template-data-reference.md` 和 `docs/theme-system-optimization-plan.md` 为准。

## 1. 动机

v5 实现了主题选项（Theme Options），但所有选项都是**全局的**——`recent_posts_count`、`saying_post_id` 等放在全局 options 里，与组件本身分离。这导致：

- 组件和它的配置不在同一个地方，心智负担重
- 同一组件不能添加多次（如两个自定义HTML）
- 后台组件页和选项页分离，配置体验割裂

v6 目标：**选项跟着组件走，组件可重复添加，后台一站式配置**。

## 2. 核心设计

```
theme.yaml 声明组件+选项 → 后台添加/排序/配置 → Setting 表存 JSON(含opts) → 模板通过 widgetOption 读取
```

- **组件声明**：`theme.yaml` 中每个 widget 可声明自己的 `options`，带 `label`（中文名）
- **可重复添加**：同一组件 ID 可多次添加到同一区域（如两个自定义HTML）
- **存储格式升级**：从 `["id1","id2"]` 升级为 `[{"id":"id1"},{"id":"id2","opts":{"k":"v"}}]`
- **模板访问**：新增 `widgetOption` 函数，读取当前渲染组件的选项
- **后台一体化**：组件页同时管理添加/排序/配置，选项页只保留全局选项

## 3. 与 v5 的关键差异

| 方面 | v5 | v6 |
|------|----|----|
| 选项归属 | 全局 options | 组件级 options |
| 组件名称 | 显示 ID | 显示 Label（中文） |
| 组件重复 | 不支持 | 支持 |
| 存储格式 | `["id1","id2"]` | `[{"id":"id1","opts":{...}}]` |
| 模板读取 | `themeOption "key"` | `widgetOption "key"` |
| 后台配置 | 组件页 + 选项页分离 | 组件页一站式 |
| 全局选项 | 所有选项 | 仅 custom_css、footer_text 等 |

## 4. theme.yaml 扩展

### WidgetDecl 新增字段

```go
type WidgetDecl struct {
    ID      string       `yaml:"id"`
    Label   string       `yaml:"label"`              // 新增：中文显示名
    Area    string       `yaml:"area"`               // 默认区域
    Options []OptionDecl `yaml:"options,omitempty"`  // 新增：组件级选项
}
```

### 示例

```yaml
widgets:
  - id: saying
    label: "博主动态"
    area: sidebar
    options:
      - id: post_id
        type: number
        label: "文章 ID"
        description: "用于读取博主评论的文章/页面 ID"
        default: "456"
        min: 1

  - id: recent_posts
    label: "近期文章"
    area: sidebar
    options:
      - id: count
        type: number
        label: "显示数量"
        default: "5"
        min: 1
        max: 20

  - id: popular_posts
    label: "热门文章"
    area: sidebar
    options:
      - id: count
        type: number
        label: "显示数量"
        default: "5"
        min: 1
        max: 20

  - id: custom_html
    label: "自定义HTML"
    area: sidebar
    options:
      - id: title
        type: text
        label: "标题"
        default: ""
      - id: content
        type: html
        label: "HTML 内容"
        default: ""

  - id: user_info
    label: "用户信息"
    area: sidebar

  - id: search
    label: "搜索"
    area: sidebar

  - id: recent_comments
    label: "近期评论"
    area: sidebar

  - id: archive_months
    label: "归档"
    area: sidebar

  - id: categories
    label: "分类目录"
    area: sidebar

  - id: tag_cloud
    label: "标签云"
    area: sidebar
```

### 选项迁移

从全局 options 移到组件 options：

| 旧全局选项 | 迁移到 |
|-----------|--------|
| `recent_posts_count` | `recent_posts.options[count]` |
| `saying_post_id` | `saying.options[post_id]` |

保留在全局 options：

| 选项 | 理由 |
|------|------|
| `custom_css` | 不属于任何组件，注入到 `<head>` |
| `footer_text` | 不属于任何组件，注入到 `<footer>` |

## 5. 存储格式

### 旧格式（v4/v5）

```json
["search","recent_posts","categories"]
```

### 新格式（v6）

```json
[
  {"id":"search"},
  {"id":"recent_posts","opts":{"count":"10"}},
  {"id":"saying","opts":{"post_id":"456"}},
  {"id":"custom_html","opts":{"title":"公告","content":"<p>欢迎</p>"}},
  {"id":"custom_html","opts":{"title":"友链","content":"<ul><li>...</li></ul>"}}
]
```

- 每个元素是对象，`id` 必填，`opts` 可选
- 同一 `id` 可出现多次（如两个 custom_html）
- `ResolveWidgets` 兼容旧格式（纯字符串数组自动升级）

## 6. 模板访问

### widgetOption 模板函数

```gohtml
{{define "widget_recent_posts"}}
{{$n := widgetOption "count" | default 5}}
<section class="widget widget-recent-posts">
  <h3>{{.t.T "近期文章"}}</h3>
  <ul>
    {{range $i, $p := .RecentPosts}}{{if lt $i $n}}
      <li><a href="{{postURL $p}}">{{$p.Title}}</a></li>
    {{end}}{{end}}
  </ul>
</section>
{{end}}
```

### saying 组件改造

```gohtml
{{define "widget_saying"}}
{{$postID := widgetOption "post_id" | default "456" | toInt}}
{{$saying := themeInvoke "saying" "n" 5 "post_id" $postID}}
{{if $saying}}
<section class="widget widget-saying">
  ...
</section>
{{end}}
{{end}}
```

`saying` 主题函数改为接受 `post_id` 参数而非从全局 option 读取。

### 实现方式

`renderWidgets` 渲染每个组件前，通过 `SetCurrentWidgetOptions(opts)` 设置当前组件的选项 map。`widgetOption` 函数从该 map 读取。

```go
var currentWidgetOptions map[string]string

func SetCurrentWidgetOptions(opts map[string]string) {
    currentWidgetOptions = opts
}

func widgetOption(key string) string {
    if currentWidgetOptions == nil {
        return ""
    }
    return currentWidgetOptions[key]
}
```

## 7. 后台页面重新设计

### 布局

```
┌─────────────────────────────────────────────────────────┐
│  可用组件                                                │
│  ┌──────────────┬──────┬──────────────────────────────┐  │
│  │ 名称         │ 来源 │ 操作                         │  │
│  ├──────────────┼──────┼──────────────────────────────┤  │
│  │ 搜索         │ 内置 │ [侧边栏 ▾] [确定]            │  │
│  │ 博主动态     │ 主题 │ [侧边栏 ▾] [确定]            │  │
│  │ 自定义HTML   │ 主题 │ [侧边栏 ▾] [确定]            │  │
│  └──────────────┴──────┴──────────────────────────────┘  │
├─────────────────────────────────────────────────────────┤
│  侧边栏区域                                              │
│  ┌────────────────────────────────────────────────────┐  │
│  │ ↑↓ │ 用户信息          │ (无选项)       │ [移除]  │  │
│  │ ↑↓ │ 近期文章 ▸        │ 显示数量: 5    │ [移除]  │  │
│  │     │   └ 显示数量 [5]  │                │         │  │  ← 点击展开
│  │ ↑↓ │ 博主动态 ▸        │ 文章ID: 456    │ [移除]  │  │
│  │     │   └ 文章ID [456]  │                │         │  │
│  │ ↑↓ │ 自定义HTML ▸      │ HTML内容: ...  │ [移除]  │  │
│  └────────────────────────────────────────────────────┘  │
├─────────────────────────────────────────────────────────┤
│  页脚区域                                                │
│  ┌────────────────────────────────────────────────────┐  │
│  │ ↑↓ │ 页脚文字        │ 文字: Powered by Blog [移除]│  │
│  └────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### 交互

- **可用组件**：每行有下拉选择区域 + 确定按钮，点击后添加到对应区域
- **区域面板**：每个组件行有 ↑↓ 排序按钮 + 移除按钮
- **折叠配置**：点击组件名称展开/收起选项表单（有选项的组件显示 ▸ 图标）
- **组件名称**：显示 `label`（中文），不是 `id`

## 8. 实现步骤

### Step 1: 扩展数据模型
- `theme.go`: WidgetDecl 加 Label + Options 字段
- `widgets.go`: WidgetInfo 加 Options，ResolveWidgets 兼容新旧格式
- `theme.yaml`: 所有组件加 label + options，移除全局 recent_posts_count 和 saying_post_id

### Step 2: 新增 widgetOption 模板函数
- `render.go`: 加 `widgetOption` + `SetCurrentWidgetOptions`
- `renderWidgets` 渲染每个组件前设置当前选项

### Step 3: 更新组件模板和主题函数
- `recent_posts.gohtml`: `themeOption` → `widgetOption "count"`
- `saying.gohtml` + `functions.goyaegi`: 改用 `widgetOption "post_id"` 传参

### Step 4: 重新设计后台页面
- `admin_widgets.go`: 新数据结构（可用组件列表 + 区域面板 + 选项）
- `admin_widgets.gohtml`: 全新 UI
- `admin.css`: 新样式

### Step 5: 更新保存逻辑
- `SaveWidgets`: 解析新格式（含 options，支持重复 ID）

### Step 6: 编译测试验证
