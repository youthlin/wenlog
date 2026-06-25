# 主题系统 v5 设计方案：主题选项（Theme Options）

> 历史设计稿：当前实现以 `AGENTS.md`、`docs/template-data-reference.md` 和 `docs/theme-system-optimization-plan.md` 为准。

## 1. 动机

v4 实现了组件开关/排序，但组件**不能带参数**。用户无法配置：
- 首页背景图
- 自定义 CSS
- 最近文章显示几条
- 纯 HTML 组件（直接输入 HTML 代码）

v5 目标：**主题声明可配置项，用户在后台填写，模板通过 `themeOption` 读取**。

## 2. 核心设计

```
theme.yaml 声明 options → 后台表单填写 → Setting 表存 JSON → 模板通过 themeOption 读取
```

- **主题声明**：`theme.yaml` 中新增 `options` 字段，每个选项有 id、type、label、default 等
- **存储**：Setting 表，key 格式 `option_<theme>_<id>`，value 为用户填写的字符串
- **模板访问**：`{{themeOption "homepage_bg"}}` 或 `{{themeOption "recent_posts_count" | default 5}}`
- **切换主题**：旧主题的 option 配置保留不删，切回时自动恢复
- **组件复用**：组件模板也可以读 option，比如 `recent_posts` 组件读 `option_<theme>_recent_posts_count`

## 3. theme.yaml 扩展

```yaml
options:
  - id: homepage_bg
    type: image
    label: "首页背景图"
    description: "留空使用默认背景"
    default: ""

  - id: custom_css
    type: css
    label: "自定义 CSS"
    description: "会注入到页面 <head> 中"
    default: ""

  - id: recent_posts_count
    type: number
    label: "最新文章显示数量"
    default: "5"
    min: 1
    max: 20

  - id: footer_text
    type: text
    label: "页脚文字"
    default: "Powered by Blog"

  - id: footer_html
    type: html
    label: "页脚 HTML"
    description: "支持任意 HTML，会原样输出"
    default: ""

  - id: accent_color
    type: color
    label: "主题色"
    default: "#3b82f6"

  - id: sidebar_position
    type: select
    label: "侧边栏位置"
    default: "right"
    options:
      - value: "left"
        label: "左侧"
      - value: "right"
        label: "右侧"
```

### 选项类型

| type | 说明 | 后台控件 | 存储格式 |
|------|------|----------|----------|
| `text` | 单行文本 | `<input type="text">` | 字符串 |
| `textarea` | 多行文本 | `<textarea>` | 字符串 |
| `number` | 数字 | `<input type="number">` | 数字字符串 |
| `image` | 图片 URL | 文本输入 + 媒体库选择按钮 | URL 字符串 |
| `color` | 颜色 | `<input type="color">` | `#rrggbb` |
| `css` | CSS 代码 | `<textarea>` + 语法提示 | CSS 字符串 |
| `html` | HTML 代码 | `<textarea>` | HTML 字符串 |
| `select` | 下拉选择 | `<select>` | 选中项的 value |
| `bool` | 开关 | `<input type="checkbox">` | `"true"` / `"false"` |

## 4. 存储

Setting 表，每个选项一条记录：

```
Key:   option_default_homepage_bg
Value: /wp-content/uploads/2026/06/bg.jpg

Key:   option_default_recent_posts_count
Value: 5

Key:   option_default_custom_css
Value: body { font-size: 16px; }
```

- key 格式：`option_<theme_name>_<option_id>`
- 用户未配置时 key 不存在，模板中用 `default` 值
- 切换主题后旧主题的 option 保留，新主题的 option 独立

## 5. 模板访问

通过已有的 `themeOption` 函数：

```gohtml
<!-- 读取选项，未配置时用 default -->
{{$count := themeOption "recent_posts_count" | default 5}}

<!-- 首页背景图 -->
{{$bg := themeOption "homepage_bg"}}
{{if $bg}}<style>body { background-image: url({{$bg}}); }</style>{{end}}

<!-- 自定义 CSS 注入 -->
{{$css := themeOption "custom_css"}}
{{if $css}}<style>{{$css | safeHTML}}</style>{{end}}

<!-- 页脚 HTML -->
{{$html := themeOption "footer_html"}}
{{if $html}}{{$html | safeHTML}}{{end}}
```

`themeOption "<id>"` 逻辑：
1. 从 Setting 表读 `option_<current_theme>_<id>`
2. 若为空，返回 `theme.yaml` 中该 option 的 `default` 值
3. 若 default 也为空，返回空字符串

## 6. 后台 UI

路径：`/admin/theme-options`（或放在 `/admin/themes` 每个主题卡片上的「自定义」按钮）

功能：
- 显示当前激活主题的所有 options
- 按 `theme.yaml` 声明渲染对应表单控件
- 保存后写入 Setting 表
- 预览按钮：保存后跳转到前台预览效果

权限：Admin 可访问。

## 7. 与组件的关系

组件模板可以读取 option 来实现参数化：

```gohtml
<!-- web/widgets/recent_posts.gohtml -->
{{define "widget_recent_posts"}}
{{$n := themeOption "recent_posts_count" | default 5}}
<section class="widget widget-recent-posts">
  <h3>{{.t.T "最新文章"}}</h3>
  <ul>
    {{range $i, $p := .RecentPosts}}
      {{if lt $i $n}}
        <li><a href="{{postURL $p}}">{{$p.Title}}</a></li>
      {{end}}
    {{end}}
  </ul>
</section>
{{end}}
```

这样不需要为每个组件单独做参数面板，统一走 option 机制。

## 8. 实现计划

1. `internal/theme/theme.go` — Theme 结构体新增 `Options []OptionDecl` 字段
2. `internal/theme/options.go` — 新增 OptionDecl 类型 + GetOption 函数
3. `internal/handler/admin_theme_options.go` — 新建后台 handler
4. `web/templates/admin_theme_options.gohtml` — 新建后台模板
5. `cmd/server/routes.go` — 注册路由
6. `cmd/server/web.go` — 注入 option provider 到 themeOption
7. `web/themes/default/theme.yaml` — 添加示例 options
8. 后台导航添加入口
