# 插件系统调研与设计

> 本文是插件系统的设计文档，目标是把"主题里顺手实现的功能"沉淀为可跨主题复用、可启停、可配置的能力。插件系统已按本文方案实现，代码位于 `internal/plugin/`、`hook/`、`cmd/server/hook.go`。

## 1. 背景

当前项目已经支持主题切换、主题选项和组件区域，但部分“功能”仍绑定在具体主题里：

- `default` 主题内置了“博主动态”组件：主题在 `theme.yaml` 声明 `saying` 组件，并通过 `functions.goyaegi` 注册 `saying` 数据函数，再由组件模板渲染，见 `web/themes/default/theme.yaml:17`、`web/themes/default/functions.goyaegi:24`、`web/themes/default/widgets/saying.gohtml:1`。
- `twentytwenty` 主题实现了评论表情：主题模板调用 `themeInvoke "render_comment"` 渲染评论正文，并调用 `themeInvoke "post-comment-enhance"` 输出表情按钮，见 `web/themes/twentytwenty/templates/comments.gohtml:21`、`web/themes/twentytwenty/templates/comments.gohtml:72`；实际表情列表与替换逻辑在 `web/themes/twentytwenty/functions.goyaegi:36`、`web/themes/twentytwenty/functions.goyaegi:48`。

这些能力从用户视角看并不属于“外观主题”，而属于“站点功能”：使用任意主题时都希望继续可用。插件系统要解决的核心问题就是：**功能归插件，外观归主题；主题可以适配插件外观，但不应独占插件能力。**

## 2. 当前实现现状

### 2.1 主题与组件模型

- 主题元数据由 `theme.Theme` 描述，包含 `WidgetAreas`、`Widgets`、`Options` 等字段，见 `internal/theme/theme.go:15`。
- 内置组件目前是 Go 代码中的静态声明，列表在 `internal/theme/widgets.go:11`，声明在 `internal/theme/widgets.go:16`。
- 后台组件页会把当前主题声明与内置组件合并后展示，见 `internal/handler/admin_widgets.go:32`；保存时将区域配置写入 `widget_<area>` 设置，见 `internal/handler/admin_widgets.go:129`。
- 渲染时 `ResolveWidgets` 根据当前主题、区域和用户配置解析出组件列表，见 `internal/theme/widgets.go:87`；模板函数 `renderWidgets` 遍历组件并执行 `widget_<id>` 模板，见 `internal/render/theme.go:337`；组件实例选项通过 `widgetOption` 获取，见 `internal/render/theme.go:379`。

这说明当前组件系统已经有插件化雏形：组件声明、组件选项、组件模板、组件区域、用户配置已经分离，只是“组件来源”目前只有“内置”和“当前主题”。插件系统可以在此基础上增加第三种来源：`plugin`。

### 2.2 主题脚本与 ThemeAPI

- 主题可以提供 `functions.go` 或 `functions.goyaegi`，由 `CompileFunctions` 编译执行，见 `internal/theme/functions.go:31`。
- 脚本通过 `themeapi.Api.RegisterFunc` 注册可在模板中调用的函数，注册入口见 `themeapi/api.go:129`。
- `themeInvoke` 的实际实现由主题运行时注入到渲染器，见 `internal/render/theme.go:288`。

这说明项目已有“运行时脚本 + 宿主 API”的经验，但该能力现在挂在主题生命周期上。插件系统可以复用思路，但需要把 API 命名、生命周期、隔离边界从 theme runtime 中独立出来。

### 2.3 前台模板数据

前台 `Public.base()` 会注入通用模板数据，例如 `RecentPosts`、`RecentCommentItems`、当前主题对象等，见 `internal/handler/public.go:408`、`internal/handler/public.go:451`、`internal/handler/public.go:456`、`internal/handler/public.go:477`。这类全局数据对主题和插件都很重要，后续插件 API 应避免让插件直接访问 GORM model 或数据库连接，而是暴露只读视图与受控写入能力。

## 3. WordPress 插件系统调研结论

本项目的主题/组件体系已经借鉴 WordPress，因此插件系统也可以参考 WordPress 的成熟边界。

### 3.1 插件包与元数据

WordPress 插件最小形态是一个带插件头注释的 PHP 文件；更常见的形态是在 `wp-content/plugins/{plugin-name}/` 下放一个主插件文件，插件头声明名称、版本、作者、许可证等元数据。WordPress 扫描插件目录后把它显示在后台插件列表。

可借鉴点：

- 插件必须有稳定 ID、名称、版本、作者、描述、许可证等 manifest。
- 插件包应自包含：脚本、模板、静态资源、翻译、默认配置放在同一目录。
- 后台插件列表不应该依赖执行插件代码才能读取基础元数据。

### 3.2 Hooks：Actions 与 Filters

WordPress 的核心扩展机制是 hooks：

- Action：在某个执行点“通知发生了某事”，回调执行副作用，不要求返回值。
- Filter：在某个数据流上“允许修改数据”，回调接收原值并返回新值。
- `add_action/add_filter` 用于注册回调，`do_action/apply_filters` 用于触发回调。

可借鉴点：

- 插件与宿主之间不应靠修改核心模板或 handler，而应靠稳定扩展点连接。
- Action 和 Filter 要分开：评论提交后发通知是 action，评论内容渲染前替换表情是 filter。
- Hook 名称应语义化、版本化、可文档化，否则会变成隐式耦合。

### 3.3 生命周期：启用、停用、卸载

WordPress 区分 activation、deactivation、uninstall：

- Activation：启用时初始化默认配置、创建表、注册必要资源。
- Deactivation：停用时清缓存、临时状态、重建规则，不删除长期数据。
- Uninstall：用户删除插件时才清理插件配置、插件表等长期数据。

可借鉴点：

- “停用”不应删除用户配置，用户稍后重新启用应该能恢复。
- “卸载”需要显式确认，并允许插件清理自己的设置和数据。
- 插件升级也应作为生命周期事件处理，便于迁移配置格式。

### 3.4 插件与主题关系

WordPress 的最佳实践是：功能放插件，展示结构/样式放主题。主题通过标准模板函数和 hook/slot 让插件接入，而不需要知道具体启用了哪些插件。

可借鉴点：

- 组件、短代码、评论增强、统计、SEO、站点地图等功能适合插件化。
- 主题不应知道真实站点启用了哪些插件；主题只需要在标准位置调用宿主提供的内容函数、组件区域和 slot/hook 点。
- 插件暴露组件时，应自行提供默认渲染实现；主题负责提供区域和整体样式，不负责认识插件组件。

### 3.5 插件提供小组件的 WordPress 流程

WordPress 的小组件体系可以拆成三段：

1. **主题声明区域**：主题通过 `register_sidebar()` 注册 widget area/sidebar，并在模板里调用 `dynamic_sidebar('sidebar-id')` 输出该区域。
2. **插件注册小组件类型**：插件定义一个继承 `WP_Widget` 的类，并在 `widgets_init` action 中调用 `register_widget('My_Widget')`。注册后，这个 widget 类型会出现在后台小组件列表里。
3. **站点管理员放置实例**：管理员把某个 widget 类型加入某个 sidebar，并保存该实例配置。

前台渲染时，主题只调用 `dynamic_sidebar()`，并不知道里面有哪些插件小组件。WordPress 会读取该 sidebar 下已配置的 widget 实例，逐个找到对应已注册 widget 的回调，最终调用该 `WP_Widget` 子类的 `widget($args, $instance)` 方法输出 HTML。也就是说：

- 主题负责：注册区域、调用 `dynamic_sidebar()`。
- 插件负责：注册 widget 类型、提供 `widget()` 前台输出、`form()` 后台表单、`update()` 保存清洗。
- WordPress 负责：保存“哪个区域有哪些 widget 实例”的配置，并在 `dynamic_sidebar()` 中调度渲染。

这个模型对本项目的启发是：`renderWidgets "area" .` 应该类似 `dynamic_sidebar('area')`；主题只调用区域渲染函数，插件组件如果被放入该区域，应由插件自己的模板或渲染 hook 输出。

## 4. 设计目标与非目标

### 4.1 目标

1. **跨主题复用功能**：`saying` 博主动态、评论表情这类能力不再绑定某个主题。
2. **可启停、可配置**：管理员能在后台安装、启用、停用、配置插件。
3. **主题与插件低耦合**：主题只按标准写法调用内容函数、组件区域和 slot/hook 点；插件自行实现自己的组件渲染。
4. **扩展点稳定**：核心流程提供 typed hooks，插件通过 hooks 介入，而不是复制整套主题模板。
5. **安全边界清晰**：插件默认由管理员安装；运行能力受 API 白名单约束；前台输出默认走 HTML 转义。
6. **兼容现有主题系统**：优先复用当前组件区域、`widgetOption`、模板解析和 ThemeAPI 的经验，避免推倒重来。

### 4.2 非目标

1. 不追求 WordPress 100% 兼容，也不引入 PHP 运行时。
2. 第一阶段不做面向第三方市场的在线安装、自动更新和签名仓库。
3. 第一阶段不允许普通访客安装或上传插件；仅后台管理员可操作。
4. 第一阶段不把所有核心功能都改成插件，只抽离已验证有跨主题价值的能力。

## 5. 插件系统总体模型

建议新增 `internal/plugin` 包，职责类似 `internal/theme`，但面向功能扩展而不是外观主题。插件的最小职责是：**注册 action / filter，介入宿主已有流程**。Widget、模板、静态资源、设置都只是插件按需声明的附加能力，不应成为插件必备结构。

```text
plugins/
├── post-comment-enhance/
│   ├── plugin.yaml
│   ├── functions.goyaegi
│   └── assets/
│       ├── style.css
│       └── smilies/*.gif
└── saying/
    ├── plugin.yaml
    ├── functions.goyaegi
    ├── widgets/
    │   └── saying.gohtml
    └── i18n/
        └── en_US.po
```

### 5.1 Manifest：`plugin.yaml`

建议格式：

```yaml
id: "post-comment-enhance"
name: "评论表情"
version: "1.0.0"
description: "为评论表单和评论正文提供表情支持"
author: "youthlin"
plugin_uri: "https://github.com/youthlin/wenlog"
license: "MIT"
requires_wenlog: ">=0.1.0"

assets:
  styles:
    - style.css

hooks:
  filters:
    - comment.render_html
    - frontend.assets
  slots:
    - comment_form.after_textarea

settings:
  - id: enabled_names
    type: textarea
    label: "启用表情"
    default: "微笑\n呲牙\n偷笑"

widgets:
  - id: saying
    label: "博主动态"
    options:
      - id: title
        type: text
        label: "标题"
        default: "博主动态"
      - id: post_id
        type: number
        label: "文章 ID"
        min: 1
```

字段说明：

| 字段 | 说明 |
|---|---|
| `id` | 插件唯一 ID，只允许小写字母、数字、短横线、下划线；用于目录名、设置 key 和资源 URL。 |
| `name/version/description/author/license` | 后台展示与升级判断用元数据。 |
| `requires_wenlog` | 可选，声明最低宿主版本，避免旧版本加载不兼容插件。 |
| `settings` | 插件全局设置声明，复用主题 `OptionDecl` 的类型体系。 |
| `hooks` | 可选，声明插件会注册或依赖的 action/filter/slot，便于后台展示和冲突检查；实际注册仍由脚本完成。 |
| `widgets` | 可选，插件提供的全局组件声明，不依赖当前主题，也不绑定具体区域。没有组件的插件可以完全省略。 |
| `templates` / `widgets` 目录 | 可选，仅当插件需要提供默认片段模板或组件模板时存在。 |
| `assets` | 可选，插件默认需要注入的 CSS/JS；通常通过 `frontend.assets` filter 按页面条件注入。 |

### 5.2 插件运行时

建议核心类型：

```go
type Plugin struct {
    ID          string
    Name        string
    Version     string
    Description string
    Dir         string
    Settings    []OptionDecl
    Widgets     []WidgetDecl
    Assets      AssetDecl
}

type Manager struct {
    dir      string
    store    *store.Store
    registry *Registry
    hooks    *Hooks
}

type Registry struct {
    filters map[string][]FilterHandler
    actions map[string][]ActionHandler
    widgets map[string]PluginWidget // 可选能力：插件通过 filter 暴露给组件系统
}
```

生命周期：

1. 启动时扫描 `plugins/` 目录，读取所有 `plugin.yaml`。
2. 从 Setting 表读取启用列表，例如 `plugins_enabled = ["post-comment-enhance", "saying"]`。
3. 仅编译启用插件的 `functions.goyaegi`，并注册 hooks、widgets、shortcodes、assets 等能力。
4. 插件加载失败时：记录错误、后台展示故障、跳过该插件，不影响核心站点启动。
5. 插件停用后：不再注册 hooks 与 widgets，但保留 `plugin_<id>_*` 设置。

## 6. Hooks 设计

### 6.1 Hook 类型

建议 Go 侧区分 action/filter：

```go
type ActionHandler func(ctx *Context, args Args) error
type FilterHandler[T any] func(ctx *Context, value T, args Args) (T, error)

func (h *Hooks) DoAction(ctx *Context, name string, args Args)
func (h *Hooks) ApplyFilter[T any](ctx *Context, name string, value T, args Args) T
```

如果 Go 泛型让脚本侧接入复杂，可以在 `pluginapi` 层暴露非泛型包装：

```go
pluginapi.Api.AddAction("comment.submitted", func(args map[string]any) {})
pluginapi.Api.AddFilter("comment.render_html", func(value any, args map[string]any) any { return value })
```

### 6.2 建议第一批扩展点

| Hook | 类型 | 触发位置 | 用途 |
|---|---|---|---|
| `plugin.activate` | action | 后台启用插件后 | 初始化默认设置、迁移数据。 |
| `plugin.deactivate` | action | 后台停用插件后 | 清理缓存、临时状态。 |
| `plugin.uninstall` | action | 后台删除插件时 | 删除插件设置和插件数据。 |
| `template.data` | filter | `Public.base()` 组装完模板数据后 | 给所有前台页面增加插件数据。 |
| `frontend.assets` | filter | 渲染 `<head>` 或 footer 前 | 注入插件 CSS/JS。 |
| `widgets.available` | filter | 后台组件页和组件渲染前构建可用组件时 | 合并插件提供的组件声明。 |
| `widget.render` | filter/action | 渲染某个来源为 plugin 的组件时 | 由插件按组件 ID 与实例选项返回组件 HTML。 |
| `widget.render_html` | filter | 单个组件渲染后 | 包装组件外层、追加样式标记。 |
| `comment_form.after_textarea` | slot | 评论表单 textarea 后 | 在 textarea 后注入表情按钮、Markdown 工具栏、验证码入口等。 |
| `comment.render_html` | filter | 评论正文输出前 | 表情替换、@ 提及、Markdown 子集等。 |
| `comment.submitted` | action | 评论保存后 | 通知、反垃圾、积分等副作用。 |

更重要的是：需要先梳理宿主和主题的“标准扩展点”。类似 WordPress 主题调用 `the_content()` 时，宿主会在内部执行内容相关 filter；本项目也应让主题作者按推荐模板函数写法接入扩展点，而不是知道具体插件。比如主题写 `{{postContent .Post}}`，而不是直接输出 `.Post.Content`；`postContent` 内部再触发 `post.content_html` filter。

`{{pluginSlot "comment_form.after_textarea" .}}` 对应 WordPress 里的典型写法是主题模板中的 `do_action('某个_slot_name', $context)`：主题只声明“这里有一个插槽”，插件用 `add_action()` 往这个位置输出内容。区别是本项目模板语言不是 PHP，所以建议提供 `pluginSlot` 模板函数来封装 `DoAction`，避免主题直接接触 Go hook 实现。

### 6.3 Hook 命名约定

- 使用小写点分命名：`comment.render_html`、`frontend.assets`。
- 名称表达“领域 + 事件/数据”，避免 `before_xxx` 滥用。
- Filter 名称应体现返回值语义，例如 `comment.render_html` 返回 HTML 字符串。
- 每个 hook 必须文档化：触发时机、参数、返回值、安全要求、是否允许错误中断主流程。

## 7. 插件组件设计

### 7.1 当前组件来源扩展

当前组件来源只有：

1. 内置组件：`internal/theme/widgets.go:16`。
2. 当前主题声明组件：`internal/theme/theme.go:37`。

建议增加：

3. 启用插件声明组件：`plugins/*/plugin.yaml` 中的 `widgets`。

但从机制上看，组件不需要成为插件 Manager 的硬编码核心能力。更干净的方式是：组件系统在“收集可用组件声明”时调用一个 filter，把当前已知组件列表交给插件修改：

```go
decls := theme.WidgetDeclsWithBuiltins(currentTheme)
decls = hooks.ApplyFilter(ctx, "widgets.available", decls, plugin.Args{
    "theme": currentTheme,
})
```

这样插件只是普通 filter 参与者，组件系统不需要提前知道全部插件类型：

- 不提供 widget 的插件，只注册自己的 action/filter 即可。
- 想提供 widget 的插件，在 `widgets.available` filter 中 append 一个组件声明；声明只描述“有哪些 widget 类型”，不描述它属于哪个区域。
- 一个 widget 如果来源是 `plugin:<id>`，就应由该插件负责渲染实现；插件可以通过 `widgets/{id}.gohtml` 模板渲染，也可以在 `widget.render` hook 中直接返回 HTML。
- 后台组件页不需要知道“插件系统内部有一个 widget registry”，只需要消费 filter 后的声明列表；管理员把 widget 实例添加到某个区域时，才决定该实例最终在哪个区域渲染。

后台组件页的可用组件类型合并顺序建议为：

```text
主题组件声明 > filter 新增/调整的组件声明 > 内置组件声明
```

同 ID 冲突时：

- 主题声明优先用于 `label/options` 等展示与配置定义。
- 插件通过 `widgets.available` filter 返回的声明优先于内置声明，允许内置能力逐步插件化。
- 后台应显示组件来源：`theme` / `plugin:<id>` / `builtin`。

区域只属于“实例配置”，不属于“widget 类型声明”。例如 `saying` 这个 widget 类型可以被管理员添加到 `sidebar`，也可以添加到 `footer`；插件声明不需要提前知道主题有哪些区域。

### 7.2 模板查找顺序

当插件提供的是可渲染组件时，渲染责任建议按“谁声明，谁实现”处理：

```text
builtin widget  -> web/widgets/{widget_id}.gohtml
theme widget    -> 当前主题 widgets/{widget_id}.gohtml
plugin widget   -> 对应插件 widgets/{widget_id}.gohtml 或 widget.render hook
```

原因：

- 主题作者开发主题时无法知道真实站点会启用哪些插件，不应被要求为插件组件写模板。
- 插件组件要跨主题可用，就必须由插件自带默认实现。
- 主题只负责输出标准组件区域，如 `{{renderWidgets "sidebar" .}}`，以及提供通用 CSS 容器样式。
- 如果以后需要“站点级覆盖插件模板”，也应作为站点管理员的高级能力，而不是主题作者的默认职责。

但这不是所有插件的要求。纯 action/filter 插件（例如只处理评论正文、只做统计、只改 SEO meta）不需要任何 widget/template。

### 7.3 WidgetInfo 扩展

当前 `WidgetInfo` 只有 `ID`、`TemplateName`、`Options`，见 `internal/theme/widgets.go:74`。建议扩展为：

```go
type WidgetInfo struct {
    ID           string
    Source       string // builtin / theme / plugin
    PluginID     string // Source == plugin 时有值
    TemplateName string
    Options      map[string]string
}
```

渲染器据此定位插件模板和插件资源；后台据此展示来源和缺失插件提示。

### 7.4 当前 `renderWidgets` 流程与插件化改造点

当前主题模板中的 `{{renderWidgets "sidebar" .}}` 是一个 Go 模板函数调用。它的现有流程是：

1. 模板解析阶段先注册占位函数，避免模板解析时报“函数不存在”，见 `internal/render/parse.go:60`。
2. 每次请求渲染时，Renderer 会 clone 模板，并把真正的 `renderWidgets` 闭包绑定到本次请求上下文，见 `internal/render/render.go:76`、`internal/render/render.go:88`。
3. `renderWidgets` 调用 `themeWidgetsProvider` 获取该区域要渲染的组件列表，见 `internal/render/theme.go:337`。
4. 当前 provider 由 `theme.Manager.BindTemplateFunctions()` 注入：读取当前主题、读取 `widget_<area>` 配置，然后调用 `ResolveWidgets(config, theme, area)`，见 `internal/theme/runtime.go:26`。
5. `ResolveWidgets` 当前只从“当前主题声明 + 内置组件”中解析出 `[]WidgetInfo`，并把模板名固定为 `widget_<id>`，见 `internal/theme/widgets.go:92`、`internal/theme/widgets.go:112`。
6. `renderWidgets` 遍历 `[]WidgetInfo`，把当前组件选项放入 `ctx.WidgetOptions`，再执行 `ctx.Template.ExecuteTemplate(&result, widget.TemplateName, data)`，见 `internal/render/theme.go:355`。

要支持插件组件，可以尽量保持主题模板写法不变：

```gohtml
{{renderWidgets "sidebar" .}}
```

改造点建议是：

1. **声明阶段**：`ResolveWidgets` 或其上游先得到主题/内置组件声明，再通过 `widgets.available` filter 让插件追加组件声明。
2. **配置阶段**：后台组件页保存的仍然是区域下的 widget 实例数组，例如：
   ```json
   [{"id":"saying","source":"plugin:saying","opts":{"post_id":"456"}}]
   ```
   如果暂时不加 `source`，也至少要能通过组件声明反查来源。
3. **解析阶段**：`WidgetInfo` 携带 `Source/PluginID/TemplateName/Options`。
4. **渲染阶段**：
   - `Source == builtin`：执行内置模板。
   - `Source == theme`：执行主题模板。
   - `Source == plugin`：调用插件渲染器；插件渲染器可执行插件自己的 `widgets/saying.gohtml`，或触发 `widget.render` 由插件返回 HTML。
5. **后处理阶段**：可选执行 `widget.render_html` filter，对所有组件最终 HTML 做统一处理。

## 8. 插件 API 设计

### 8.1 `pluginapi` 与 `themeapi` 的关系

`themeapi` 当前是主题脚本专用 API。插件应新增独立的 `pluginapi`，避免主题和插件生命周期混在一起。

建议原则：

- `themeapi`：给主题做展示辅助，偏只读、偏模板数据。
- `pluginapi`：给插件注册能力，包含 hooks、settings、widgets、assets、有限数据访问。
- 两者共用底层只读视图结构，避免重复定义 `PostView`、`CommentView`。

### 8.2 API 能力分层

| 能力 | 第一阶段是否开放 | 说明 |
|---|---:|---|
| 读取公开文章/页面/评论/分类/标签 | 是 | 复用 `DataLoader` 只读视图。 |
| 读取插件自身设置 | 是 | 只允许读取 `plugin_<id>_*` 命名空间。 |
| 写入插件自身设置 | 仅后台/生命周期 | 前台请求不建议任意写 Setting。 |
| 注册 action/filter | 是 | 插件核心能力。 |
| 注册 widget | 是 | 支持跨主题组件。 |
| 注入 CSS/JS asset | 是 | 必须走受控 URL。 |
| i18n 翻译 | 是 | 插件/主题 hook 可能输出按钮、标题、提示等字符串字面量，必须能按当前请求语言翻译。 |
| 注册公开路由 | 暂缓 | 路由扩展安全和冲突复杂，放 P2。 |
| 任意 DB 访问 | 否 | 避免破坏数据模型和权限边界。 |
| 任意文件读写 | 否 | 只允许插件自身 assets/templates，且运行时只读。 |

### 8.3 i18n 能力

插件和主题的 `functions.go/functions.goyaegi` 不一定只返回数据，也可能通过 action/slot/widget 渲染直接输出 UI 字符串，例如按钮文案、表单提示、组件标题、错误信息。因此 `themeapi` / `pluginapi` 都应该暴露请求级 i18n 能力。

建议原则：

1. **按来源隔离文本域**：
   - 主题文本域：`theme_<theme_id>`，沿用当前主题 i18n 设计。
   - 插件文本域：`plugin_<plugin_id>`。
2. **请求级语言**：翻译函数必须绑定当前请求语言，不能在插件加载阶段固定语言。
3. **API 暴露常用翻译函数**：至少提供 `T`、`N`、`X`、`XN`，并支持格式化参数。
4. **模板与脚本一致**：插件模板里可以用 `.tp.T` 或 `pluginT`；插件脚本里可以用 `pluginapi.Api.T(...)`。
5. **字面量标记**：插件 manifest、脚本和模板中的默认中文字符串应能被提取到 `.pot/.po`，后续需要给插件补提取工具。

示例：

```go
func Register() {
    pluginapi.Api.AddAction("comment_form.after_textarea", func(args map[string]any) {
        label := pluginapi.Api.T("表情")
        pluginapi.Api.WriteHTML(`<div class="smilies-label">` + pluginapi.Api.EscapeHTML(label) + `</div>`)
    })
}
```

更推荐插件组件使用模板输出文案，由渲染上下文注入插件文本域 translator：

```gohtml
<section class="widget widget-saying">
  <h3>{{.tp.T "博主动态"}}</h3>
</section>
```

安全注意：翻译函数只负责返回文本，不应自动标记为 HTML 安全；包含 HTML 的翻译文案必须明确经过转义和安全输出流程。

### 8.4 脚本运行方式

短期建议复用 yaegi 经验，但要比主题脚本更谨慎：

1. 插件脚本文件名使用 `functions.goyaegi` 或 `plugin.goyaegi`。
2. 脚本必须定义 `package plugin` 和无参 `Register()`。
3. 宿主注入 `pluginapi.Api`，插件通过它注册 hooks/widgets。
4. 第一阶段仍视插件为“管理员安装的可信代码”，不承诺强沙箱。
5. 中长期可评估 WASM 插件或编译期 Go 插件接口，但不要作为第一阶段前置条件。

示例：

```go
package plugin

import "pluginapi"

func Register() {
    pluginapi.Api.AddFilter("comment.render_html", func(value any, args map[string]any) any {
        content, _ := value.(string)
        return renderSmilies(content)
    })
}
```

## 9. 两个目标插件的落地设计

### 9.1 博主动态插件：`saying`

目标：把 `default` 主题的博主动态组件变成任意主题可用。

插件包：

```text
plugins/saying/
├── plugin.yaml
├── functions.goyaegi
├── widgets/saying.gohtml
└── assets/saying.css
```

实现方式：

1. `plugin.yaml` 声明插件元数据、`saying` widget 的 `post_id/count/title` 选项，以及它需要参与 `widgets.available` / `widget.render`。
2. `functions.goyaegi` 在 `widgets.available` filter 中追加 `saying` 组件声明，来源标记为 `plugin:saying`。
3. 后台组件页读取当前主题区域后，对可用组件列表应用 `widgets.available` filter，于是管理员能把 `saying` 放入 `sidebar/footer` 等区域。
4. 后台保存区域配置，例如：
   ```json
   [{"id":"saying","source":"plugin:saying","opts":{"title":"博主动态","post_id":"456","count":"5"}}]
   ```
5. 前台主题仍只写 `{{renderWidgets "sidebar" .}}`。`renderWidgets` 解析到这个实例来源是 `plugin:saying` 后，调用该插件渲染器。
6. 插件渲染器通过只读 API 找到指定文章/页面的评论，生成 `CommentURL`、`Snippet`、`AuthorName`、`AuthorEmail`，再执行插件自己的 `widgets/saying.gohtml`，或通过 `widget.render` 返回 HTML。

这个流程与 WordPress 的类比是：

- `widgets.available` 近似插件在 `widgets_init` 上 `register_widget()`。
- `renderWidgets "sidebar" .` 近似主题调用 `dynamic_sidebar('sidebar')`。
- 插件的 `widgets/saying.gohtml` / `widget.render` 近似 `WP_Widget::widget($args, $instance)`。

注意点：

- `post_id` 默认值不应硬编码为个人站点 ID；插件启用后可以为空，后台提示用户选择。
- 组件缺少 `post_id` 时前台静默不渲染，后台显示配置提示。
- 评论摘要必须 HTML 转义或使用安全摘要函数，避免把评论原文 HTML 注入到侧边栏。
- 主题不需要知道 `saying` 存在，只需要声明组件区域并调用 `renderWidgets`。

### 9.2 评论表情插件：`post-comment-enhance`

目标：把 `twentytwenty` 主题里的评论表情按钮和评论正文表情渲染变成任意主题可用。

插件包：

```text
plugins/post-comment-enhance/
├── plugin.yaml
├── functions.goyaegi
├── assets/style.css
└── assets/smilies/*.gif
```

实现方式：

1. `comment.render_html` filter：接收已转义或待渲染的评论正文，替换 `[/微笑]` 这类短码为 `<img class="smiley" ...>`。
2. `comment_form.after_textarea` slot：在评论 textarea 后注入表情按钮片段，按钮写入短码到 textarea。
3. `frontend.assets` filter：仅在文章/页面详情且评论开启时注入 CSS/JS。
4. 插件设置声明启用哪些表情、是否显示表情面板、图片尺寸等。

评论渲染安全建议：

- 核心先对评论原文做 HTML 转义，再把转义后的字符串交给 filter。
- 插件只替换已知短码，不处理任意 HTML。
- 表情图片 URL 必须来自插件 assets 目录，通过 `/plugin-assets/{plugin_id}/...` 受控输出。
- `alt/title` 必须转义。

主题适配建议：

- 核心评论模板应提供标准 slot，例如 `comment_form_after_textarea`。
- 主题可以覆盖评论模板，但应按“推荐主题写法”调用统一 slot 函数，例如 `{{pluginSlot "comment_form_after_textarea" .}}`；主题不需要知道这个 slot 最终会被评论表情、验证码还是其它插件使用。
- 若主题完全自定义评论模板且不调用 slot，插件仍能通过 `comment.render_html` 生效，但表情按钮不会自动出现；后台可提示“当前主题未声明评论表单 slot”。

## 10. 标准 Hook 点梳理

插件系统的真正难点不是“插件怎么写”，而是宿主和主题要在哪些稳定位置暴露扩展点。建议把 hook 点分成两类：

1. **宿主强制 hook**：由 Go handler / render 层统一触发，主题绕不过去。适合内容过滤、数据保存、评论提交等核心流程。
2. **主题推荐 slot**：主题模板需要按推荐写法显式调用。适合在页面某个视觉位置插入 UI，例如评论表单 textarea 后、文章内容前后、页脚前等。

### 10.1 内容渲染相关

| Hook / 函数 | 类型 | 建议调用方 | 说明 |
|---|---|---|---|
| `post.content_html` / `postContent` | filter / 模板函数 | 宿主模板函数 | 类似 WordPress `the_content()`；主题应调用 `{{postContent .Post}}`，而不是直接 `safeHTML .Post.Content`。 |
| `post.excerpt_html` / `postExcerpt` | filter / 模板函数 | 宿主模板函数 | 摘要渲染，给短代码、摘要追加等插件使用。 |
| `page.content_html` | filter | 宿主模板函数 | 页面正文渲染。可与 `post.content_html` 共用实现，但 hook 名称可保留语义。 |
| `comment.render_html` / `commentContent` | filter / 模板函数 | 宿主模板函数 | 评论正文渲染，评论表情就是典型使用方。 |

建议：这些地方最好不要要求主题直接调用 `applyFilter`，而是提供稳定模板函数。主题作者只要按文档写 `postContent/commentContent`，插件就能工作。

### 10.2 页面结构 slot

| Slot | 建议调用位置 | 典型用途 |
|---|---|---|
| `head.end` | `</head>` 前 | SEO meta、插件 CSS、preload。 |
| `body.start` | `<body>` 后 | 统计脚本 noscript、全局提示。 |
| `body.end` | `</body>` 前 | 插件 JS、统计脚本。 |
| `post.before` | 文章正文前 | 文章提示、版权提示、广告。 |
| `post.after` | 文章正文后 | 分享按钮、相关文章、版权提示。 |
| `comment_form.before` | 评论表单前 | 登录提示、反垃圾提示。 |
| `comment_form.after_textarea` | 评论 textarea 后 | 表情按钮、Markdown 工具栏、验证码入口。 |
| `comment_form.after` | 评论表单后 | 额外说明、订阅入口。 |
| `footer.before` | 页脚前 | 全站横幅、备案增强。 |

这些 slot 需要主题模板显式调用，例如：

```gohtml
{{pluginSlot "post.before" .}}
{{postContent .Post}}
{{pluginSlot "post.after" .}}
```

### 10.3 组件系统 hook

| Hook | 类型 | 调用方 | 说明 |
|---|---|---|---|
| `widgets.available` | filter | 后台组件页、前台组件解析 | 插件向可用组件列表追加声明。 |
| `widget.render` | filter/action | `renderWidgets` | 对来源为 plugin 的组件，由插件返回 HTML。 |
| `widget.render_html` | filter | `renderWidgets` | 对任意组件最终 HTML 做后处理，例如统一埋点、外层包装。 |

组件系统的原则：主题只声明区域并调用 `renderWidgets`；插件组件由插件自行渲染；内置组件由内置模板渲染；主题组件由主题模板渲染。

### 10.4 数据与生命周期 hook

| Hook | 类型 | 说明 |
|---|---|---|
| `template.data` | filter | 前台模板数据组装后，插件可追加只读展示数据。 |
| `comment.submitted` | action | 评论保存后，用于通知、反垃圾异步处理等。 |
| `post.saved` | action | 后台文章保存后，用于清缓存、生成索引。 |
| `plugin.activate` | action | 插件启用时初始化配置。 |
| `plugin.deactivate` | action | 插件停用时清临时状态。 |
| `plugin.uninstall` | action | 插件卸载时清理长期数据。 |

### 10.5 推进建议

第一批不需要把所有 hook 都实现。建议按现有诉求优先实现：

1. `widgets.available` + `widget.render`：支撑“博主动态”插件组件。
2. `comment.render_html` + `comment_form.after_textarea` slot：支撑评论表情。
3. `head.end` / `body.end`：支撑插件 CSS/JS 注入。
4. `post.content_html` / `postContent`：建立类似 `the_content()` 的标准内容出口，后续短代码、目录、版权提示都能复用。

新增 hook 的原则：

- **最小必要**：没有明确插件场景就不加；不要因为“未来可能有用”提前铺太多点。
- **文档先行**：每个 hook/slot 必须在本文或后续专门的 hook reference 中记录名称、类型、调用位置、参数、返回值、安全约束和示例。
- **稳定性分级**：第一批可标注为 experimental；一旦有插件依赖并发布，再提升为 stable，避免随意改名或改参数。
- **主题推荐写法同步**：凡是需要主题调用的 slot，都必须同步到主题开发文档和内置主题模板中，避免主题作者漏掉标准出口。
- **优先宿主函数封装**：内容类 filter 优先做成 `postContent/commentContent` 这类模板函数，由宿主内部触发 filter；只有视觉插入点才暴露为 `pluginSlot`。

## 11. 后台管理设计

### 11.1 插件列表页

新增 `/admin/plugins`：

| 列 | 说明 |
|---|---|
| 插件 | 名称、描述、版本、作者。 |
| 状态 | 已启用 / 已停用 / 加载失败。 |
| 操作 | 启用、停用、设置、删除。 |
| 依赖 | 宿主版本、缺失资源、冲突 hook。 |

### 11.2 插件设置页

新增 `/admin/plugins/:id/settings`：

- 复用主题选项的 `OptionDecl` 渲染能力。
- 设置 key 使用 `plugin_<id>_<option>`。
- 插件停用后仍保留设置；卸载时才删除。

### 11.3 安装与升级

第一阶段建议：

- 支持上传 zip 安装到 `plugins/`。
- 与主题 zip 一样做路径穿越、文件数量、解压后大小、扩展名白名单限制。
- 安装前解析 `plugin.yaml`，校验 `id` 与目录名一致。
- 覆盖安装时先解压到临时目录，校验通过后再替换，避免破坏已有插件。

## 12. 数据与存储设计

建议新增或复用 Setting：

| Key | Value | 说明 |
|---|---|---|
| `plugins_enabled` | JSON string array | 启用插件 ID 列表。 |
| `plugin_<id>_<option>` | string | 插件全局设置。 |
| `plugin_<id>_version` | string | 已安装/已迁移版本。 |

是否新增表：

- 第一阶段不必新增 `plugins` 表；插件元数据来自磁盘 manifest，启用状态和设置存在 Setting 即可。
- 如果后续需要记录安装来源、升级通道、错误状态、数据迁移历史，再考虑新增 `plugin_records` 表。

## 13. 资源路由设计

新增插件资源路由：

```text
/plugin-assets/{plugin_id}/{path}
```

规则：

- 只能访问已安装插件 `assets/` 目录下文件。
- `plugin_id` 必须是合法 ID，不能包含路径分隔符。
- 使用 `filepath.Clean` + `filepath.Rel` 校验路径不能逃逸目录。
- 只允许静态资源扩展名：`.css`、`.js`、`.png`、`.jpg`、`.jpeg`、`.gif`、`.svg`、`.webp`、`.woff2` 等。
- 静态资源不要求插件启用也能访问还是仅启用可访问，需要产品决策；建议仅启用插件可访问，减少暴露面。

## 14. 安全模型

插件系统会显著扩大攻击面，必须明确边界：

1. **安装权限**：只有管理员可上传、启用、停用、删除插件。
2. **可信代码假设**：第一阶段插件脚本不是强沙箱，等同管理员安装的可信扩展；后台必须明确提示。
3. **API 白名单**：插件不能拿到 `*gorm.DB`、`*store.Store`、任意文件系统写权限。
4. **输出安全**：filter 默认处理普通字符串；只有明确命名为 `_html` 的 hook 才允许返回 HTML，且文档中必须说明输入是否已转义。
5. **资源隔离**：插件资源必须通过受控路由输出，禁止插件返回本地绝对路径。
6. **失败隔离**：插件 panic 或 hook 超时不应拖垮站点；可对单个 hook 设置超时和 recover。
7. **CSRF 与后台 POST**：插件设置页和插件操作沿用现有后台 CSRF 约定。
8. **卸载确认**：卸载会删除设置和插件数据，必须二次确认。

## 15. 与主题系统的分工

| 事项 | 主题负责 | 插件负责 |
|---|---|---|
| 页面整体结构 | 是 | 否 |
| 颜色、排版、响应式样式 | 是 | 插件提供最小默认样式 |
| 组件区域声明 | 是 | 可建议适配区域，但不定义主题布局 |
| 组件能力与数据 | 可少量自定义 | 是 |
| 评论表情、短代码、SEO、站点地图 | 否 | 是 |
| 渲染模板 | 主题组件由主题提供 | 插件组件由插件提供默认实现 |
| 功能设置 | 主题外观设置 | 插件功能设置 |

核心原则：**主题可以让插件更好看，插件让功能一直存在。**

## 16. 渐进式实施路线

### P0：最小可用插件框架

1. 新增 `internal/plugin`：扫描 `plugins/`、读取 `plugin.yaml`、管理启用列表。
2. 新增后台 `/admin/plugins` 列表页，支持启用/停用。
3. 新增插件设置读取/保存：`plugin_<id>_<option>`。
4. 新增 `/plugin-assets/*` 资源路由。
5. 插件加载失败不影响站点启动，后台显示错误。

验收：可以安装一个只有 CSS/设置的演示插件，启用后前台能加载资源，停用后资源不再注入。

### P1：插件组件

1. 在后台组件页和前台组件解析处增加 `widgets.available` filter，把当前组件声明列表交给启用插件调整。
2. 插件如需新增组件，通过该 filter append 组件声明，并标记来源 `plugin:<id>`。
3. 对来源为 plugin 的组件，`renderWidgets` 调用对应插件的模板或 `widget.render` hook 完成渲染；纯 filter 插件不需要提供任何模板。
4. 把 `default` 的 `saying` 迁成内置示例插件或仓库内置插件。

验收：任意主题只要有组件区域，都能添加“博主动态”；该组件由 `saying` 插件自己的模板或渲染 hook 输出。

### P2：核心 Hooks 与评论表情

1. 实现 `Hooks`：action/filter 注册、优先级、错误隔离、超时。
2. 增加 `comment.render_html`、`comment_form.after_textarea`、`frontend.assets` 等 hook/slot。
3. 将 `twentytwenty` 评论表情迁移成 `post-comment-enhance` 插件。
4. 核心评论模板补 slot；现有主题逐步适配 slot。

验收：切换到任意主题后，评论正文表情渲染保持可用；支持 slot 的主题显示表情面板。

### P3：安装包、安全与迁移

1. 支持插件 zip 上传、替换安装、卸载清理。
2. 增加插件版本迁移 hook。
3. 增加插件 hook 文档页和开发者参考。
4. 为常见扩展点补测试。

验收：插件启停、升级、卸载流程可回归；恶意 zip 路径穿越被拒绝。

## 17. 测试建议

### 单元测试

- `plugin.LoadPlugin`：manifest 解析、非法 ID、缺少字段、路径校验。
- `plugin.Manager`：启用列表读取、停用不删除设置、加载失败隔离。
- `Hooks`：action/filter 顺序、优先级、错误处理、panic recover。
- `ResolveWidgets`：插件组件与主题/内置组件合并、同 ID 优先级、缺失模板处理。
- 插件资源路由：正常文件、路径穿越、非法扩展名、未启用插件。

### 集成/浏览器验收

- 默认主题启用 `saying` 插件，侧边栏显示博主动态。
- `twentytwenty`、`spread`、`single` 切换后，插件组件配置不丢失。
- 启用 `post-comment-enhance` 后评论表单出现表情按钮，提交后正文渲染为图片。
- 停用 `post-comment-enhance` 后历史评论仍显示短码文本，不报错。
- 插件加载失败时前台仍可访问，后台插件页显示错误详情。

## 18. 风险与权衡

| 风险 | 说明 | 缓解 |
|---|---|---|
| 插件脚本安全 | yaegi 不是强沙箱，插件可执行较复杂逻辑。 | 仅管理员安装；API 白名单；后续评估 WASM。 |
| Hook 过多导致维护困难 | 扩展点随意增加会形成隐式公共 API。 | 每个 hook 必须有文档、测试和稳定性级别。 |
| 主题不适配 slot | 评论表情按钮等 UI 可能无法插入。 | 插件能力分层：内容 filter 先可用，UI slot 渐进适配。 |
| 组件样式不协调 | 插件默认 HTML 在不同主题下可能不好看。 | 插件输出尽量使用通用 `.widget` 结构和最小样式；主题提供区域和通用组件容器样式。 |
| 同 ID 冲突 | 主题、插件、内置组件可能重名。 | 明确优先级；后台显示来源；日志提示冲突。 |
| 性能退化 | 多个插件 filter 串行执行影响渲染。 | hook 超时、按页面条件注入、缓存插件 manifest。 |

## 19. 建议优先结论

建议采用“先组件、后 hooks”的渐进式方案：

1. **先做插件 manifest、启停、设置和资源路由**，建立插件基本生命周期。
2. **优先把博主动态迁成插件组件**，因为它最贴近现有组件系统，改动边界清晰。
3. **再做评论表情所需 hooks/slots**，因为它涉及评论正文安全、评论表单插槽和资产注入，设计复杂度更高。
4. **不要一开始追求强沙箱或插件市场**，先把宿主扩展点、后台管理和跨主题复用跑通。

最终形态应该是：主题负责“页面长什么样”，插件负责“站点能做什么”。这样用户切换主题时，博主动态、评论表情、后续 SEO/站点地图/短代码等功能都能保持连续可用。

## 20. 具体技术实现方案

本节把前面的讨论收敛为可直接开始编码的实施方案。目标是先做“能支撑 saying 插件组件 + 评论表情 hook/slot 的最小插件系统”，避免一次性铺开过多扩展点。

### 20.1 第一阶段范围

第一阶段只实现以下能力：

1. 插件 manifest 扫描、启用列表、运行时加载。
2. 统一 Hook Registry：支持 action/filter、priority、来源追踪、panic recover。
3. 主题和插件 `functions.goyaegi` 都可以注册 action/filter，但记录不同来源。
4. 最小 hook/slot：
   - `widgets.available`
   - `widget.render`
   - `widget.render_html`
   - `comment.render_html`
   - `comment_form.after_textarea`
   - `head.end`
   - `body.end`
   - `post.content_html`
5. 最小主题模板函数：`postContent`、`commentContent`、`pluginSlot`、`renderWidgets`。
6. 插件组件：支持 `saying` 这类由插件声明并自行渲染的组件。
7. 插件 i18n：插件脚本和插件模板能按当前请求语言翻译字符串。
8. 插件静态资源路由：`/plugin-assets/{plugin_id}/{path}`。

第一阶段暂不做：

- 插件 zip 上传安装。
- 插件卸载清理 UI。
- 插件公开路由注册。
- 强沙箱 / WASM。
- 大量通用 hook 点。

### 20.2 新增包与核心文件

建议新增：

```text
internal/plugin/
├── manifest.go       # Plugin、WidgetDecl、AssetDecl、LoadPlugin
├── manager.go        # 扫描 plugins/、启用列表、加载/停用插件
├── hooks.go          # Hook Registry、Action/Filter、priority、来源追踪
├── runtime.go        # 编译插件 functions.goyaegi、注入 pluginapi
├── render.go         # 插件组件模板渲染、pluginSlot 输出辅助
├── assets.go         # 插件静态资源路径校验
└── i18n.go           # 插件文本域绑定与请求级 translator

pluginapi/
└── api.go            # 暴露给插件 functions.goyaegi 的 API
```

建议调整：

| 文件 | 改动 |
|---|---|
| `cmd/server/web.go` | 初始化 plugin.Manager，注册 `/plugin-assets/*filepath`，把 plugin runtime 接入 render/theme。 |
| `cmd/server/routes.go` | 增加 `/admin/plugins` 最小只读/启停路由；若第一阶段不做 UI，可先只初始化默认启用内置插件。 |
| `internal/render/parse.go` | 增加模板占位函数：`pluginSlot`、`postContent`、`commentContent`。 |
| `internal/render/render.go` | clone 模板时绑定真实 `pluginSlot/postContent/commentContent` 请求级闭包。 |
| `internal/render/theme.go` | 增加 hook provider / slot provider，或通过统一 runtime 调用 plugin.Manager。 |
| `internal/theme/functions.go` | 主题 `functions.goyaegi` 注入 hook 注册能力，允许 `themeapi.Api.AddAction/AddFilter`。 |
| `internal/theme/widgets.go` | `WidgetInfo` 增加 `Source/PluginID`；解析组件时支持插件声明。 |
| `internal/handler/admin_widgets.go` | 可用组件列表应用 `widgets.available` filter，显示来源。 |
| `web/themes/*/templates/*.gohtml` | 把关键位置改成推荐写法，如 `postContent`、`commentContent`、`pluginSlot`。 |

### 20.3 数据结构设计

#### 20.3.1 插件 manifest

```go
type Plugin struct {
    ID          string       `yaml:"id" json:"id"`
    Name        string       `yaml:"name" json:"name"`
    Version     string       `yaml:"version" json:"version"`
    Description string       `yaml:"description" json:"description"`
    Author      string       `yaml:"author" json:"author"`
    Dir         string       `yaml:"-" json:"-"`
    Hooks       HookDecl     `yaml:"hooks" json:"hooks"`
    Widgets     []WidgetDecl `yaml:"widgets" json:"widgets"`
    Settings    []OptionDecl `yaml:"settings" json:"settings"`
    Assets      AssetDecl    `yaml:"assets" json:"assets"`
}

type HookDecl struct {
    Actions []string `yaml:"actions" json:"actions"`
    Filters []string `yaml:"filters" json:"filters"`
    Slots   []string `yaml:"slots" json:"slots"`
}

type WidgetDecl struct {
    ID      string       `yaml:"id" json:"id"`
    Label   string       `yaml:"label" json:"label"`
    Options []OptionDecl `yaml:"options" json:"options"`
    Source  string       `yaml:"-" json:"source"` // plugin:<id>
}
```

插件 ID 校验：只允许 `[a-z0-9_-]+`，并要求目录名与 ID 一致。

#### 20.3.2 Hook Registry

```go
type Source struct {
    Type string // core / theme / plugin
    ID   string // default / twentytwenty / saying
}

type Handler struct {
    Name     string
    Priority int
    Source   Source
    Fn       any
}

type Registry struct {
    actions map[string][]Handler
    filters map[string][]Handler
}
```

建议 priority 常量：

```go
const (
    PriorityEarly   = 5
    PriorityDefault = 10
    PriorityLate    = 20
)
```

同 priority 下按加载顺序执行。建议加载顺序：

1. core 内置默认 hook。
2. 启用插件 hook。
3. 当前主题 hook。

这样主题可以做展示层最后适配，但不建议主题承载跨主题功能。

#### 20.3.3 WidgetInfo

```go
type WidgetInfo struct {
    ID           string
    Source       string // builtin / theme / plugin
    PluginID     string
    TemplateName string
    Options      map[string]string
}
```

后台保存建议逐步从：

```json
[{"id":"recent_posts","opts":{"count":"5"}}]
```

升级到：

```json
[{"id":"saying","source":"plugin:saying","opts":{"post_id":"456","count":"5"}}]
```

兼容策略：旧配置没有 `source` 时，按当前主题声明、插件声明、内置声明的合并结果反查来源。

### 20.4 pluginapi / themeapi 设计

插件和主题共用底层 hook registry，但暴露不同 API 入口，便于记录来源。

```go
type API struct {
    source Source
    hooks  *plugin.Registry
    ctx    *RequestContext
}

func (api *API) AddAction(name string, fn ActionFunc, priority ...int)
func (api *API) AddFilter(name string, fn FilterFunc, priority ...int)
func (api *API) T(msg string, args ...any) string
func (api *API) N(singular, plural string, n int, args ...any) string
func (api *API) X(ctx, msg string, args ...any) string
func (api *API) XN(ctx, singular, plural string, n int, args ...any) string
func (api *API) EscapeHTML(s string) string
```

注意：

- `AddAction/AddFilter` 在插件/主题加载阶段使用。
- `T/N/X/XN` 是请求级能力；执行 hook 时 API 要绑定当前请求 translator。
- 插件文本域为 `plugin_<plugin_id>`；主题文本域为 `theme_<theme_id>`。
- 翻译函数只返回普通字符串，不自动 safeHTML。

### 20.5 主题模板函数设计

可以进一步把主题模板常见样板抽象为宿主模板函数，方向类似 WordPress 的模板标签。这样主题作者只负责页面结构和样式，不必在每个主题里重复手写“标题、正文、上下篇、评论表单、侧边栏、页头页脚”等复杂 HTML。

建议把模板函数分两层：

1. **第一阶段必须实现的 hook 出口函数**：直接服务插件系统，例如 `postContent/commentContent/pluginSlot/renderWidgets`。
2. **第二阶段可逐步补齐的主题辅助函数**：减少主题样板，例如 `siteHeader/siteFooter/postNavigation/commentsTemplate`。

新增/规划模板函数：

| 模板函数 | 作用 | 类比 WordPress |
|---|---|---|
| `postContent .Post` | 输出文章正文并应用 `post.content_html` filter | `the_content()` |
| `commentContent .` | 输出评论正文并应用 `comment.render_html` filter | 评论内容 filter |
| `pluginSlot "slot.name" .` | 在模板中触发一个 action slot，收集 HTML 输出 | `do_action('slot_name', ...)` |
| `renderWidgets "area" .` | 渲染某个组件区域 | `dynamic_sidebar('area')` |
| `siteHeader .` | 渲染标准 header，可内部触发 `head.end` / `body.start` slot | `get_header()` |
| `siteFooter .` | 渲染标准 footer，可内部触发 `footer.before` / `body.end` slot | `get_footer()` |
| `postTitle .Post` | 输出文章标题，可统一转义和过滤 | `the_title()` |
| `postExcerpt .Post` | 输出摘要并应用 `post.excerpt_html` filter | `the_excerpt()` |
| `postNavigation .Post` | 输出上一篇/下一篇导航 | `the_post_navigation()` |
| `commentsTemplate .` | 输出标准评论列表与评论表单 | `comments_template()` |

推荐主题写法：

```gohtml
{{pluginSlot "post.before" .}}
{{postTitle .Post}}
{{postContent .Post}}
{{postNavigation .Post}}
{{pluginSlot "post.after" .}}

{{commentsTemplate .}}

<textarea id="comment-content" name="content"></textarea>
{{pluginSlot "comment_form.after_textarea" .}}

<aside class="sidebar">
  {{renderWidgets "sidebar" .}}
</aside>
```

这样一个文章页模板可以逐步简化成接近 WordPress 的写法：

```gohtml
{{siteHeader .}}

<main class="site-main">
  <article class="post">
    {{postTitle .Post}}
    {{pluginSlot "post.before" .}}
    {{postContent .Post}}
    {{pluginSlot "post.after" .}}
    {{postNavigation .Post}}
  </article>

  {{commentsTemplate .}}
</main>

{{renderWidgets "sidebar" .}}
{{siteFooter .}}
```

注意边界：

- `postContent/commentContent` 这类函数是插件系统第一阶段必需，因为它们是内容 filter 的稳定出口。
- `commentsTemplate` 可以显著减少主题重复 HTML，但也会限制主题对评论结构的自由度；建议先提供默认实现，主题仍可选择自定义评论模板，只要保留必要 slot。
- `siteHeader/siteFooter` 可先作为推荐方向，不必阻塞插件系统第一阶段落地。
- 所有这些函数都应在文档中维护“会触发哪些 hook/slot”，避免主题作者不知道哪些插件能力依赖它们。

### 20.6 插件组件 saying 的具体流程

#### 20.6.1 插件声明

`plugins/saying/plugin.yaml`：

```yaml
id: saying
name: 博主动态
version: 1.0.0
description: 提供博主动态组件
hooks:
  filters:
    - widgets.available
  actions:
    - widget.render
widgets:
  - id: saying
    label: 博主动态
    options:
      - id: title
        type: text
        label: 标题
        default: 博主动态
      - id: post_id
        type: number
        label: 文章 ID
        min: 1
      - id: count
        type: number
        label: 显示数量
        default: "5"
        min: 1
        max: 20
```

#### 20.6.2 后台声明合并

1. 后台 `/admin/widgets` 读取当前主题区域。
2. 构造当前可用组件：主题组件 + 内置组件。
3. 调用：`widgets.available` filter。
4. `saying` 插件 append 自己的 WidgetDecl，来源为 `plugin:saying`。
5. 后台下拉列表出现“博主动态”，来源显示“插件：saying”。
6. 管理员把它加入 `sidebar` 并保存选项。

#### 20.6.3 前台渲染

主题模板仍然只写：

```gohtml
{{renderWidgets "sidebar" .}}
```

渲染流程：

1. `renderWidgets` 读取 `widget_sidebar` 配置。
2. 解析出 `WidgetInfo{ID:"saying", Source:"plugin", PluginID:"saying", Options:...}`。
3. `renderWidgets` 发现来源为 plugin，调用 plugin.Manager 渲染该组件。
4. plugin.Manager 为 `saying` 插件创建请求级上下文：DataLoader、translator、组件 options。
5. 插件读取 `post_id/count`，生成动态列表数据。
6. 插件执行 `plugins/saying/widgets/saying.gohtml`，模板中可用 `.tp.T` 翻译文案。
7. 返回 HTML 给 `renderWidgets` 汇总输出。
8. 可选执行 `widget.render_html` filter 做最终后处理。

#### 20.6.4 失败策略

- 插件未启用：后台不再显示该组件；旧配置在前台跳过，后台提示“组件来源插件未启用”。
- 插件启用但模板缺失：前台跳过该组件并记录日志；后台插件页显示错误。
- `post_id` 为空：前台不渲染；后台组件配置处提示需要填写。

### 20.7 评论表情的具体流程

1. 主题评论模板按推荐写法输出评论正文：`{{commentContent .}}`。
2. `commentContent` 先对评论原文做 HTML 转义，再应用 `comment.render_html` filter。
3. `post-comment-enhance` 插件把 `[/微笑]` 替换成受控 `/plugin-assets/post-comment-enhance/smilies/微笑.gif` 图片。
4. 主题评论表单 textarea 后调用：`{{pluginSlot "comment_form.after_textarea" .}}`。
5. `post-comment-enhance` 插件在该 slot 输出表情按钮面板。
6. `head.end/body.end` 注入插件 CSS/JS。

### 20.8 编码顺序

建议按以下顺序编码，确保每一步都能独立验证：

| 状态 | 步骤 | 内容 | 当前说明 |
|---|---:|---|---|
| done | 1 | **Hook Registry**：实现 `internal/plugin/hooks.go`，补 action/filter priority、来源、recover 测试。 | 已实现 action/filter 注册、priority 排序、来源记录、panic recover；当前以编译和集成测试验证，后续可补更细单测。 |
| done | 2 | **Manifest + Manager**：实现插件扫描、启用列表读取、manifest 校验。 | 已实现 `plugin.yaml` 解析、插件 ID/目录校验、启用列表读取、启停保存、启用插件运行时加载。 |
| done | 3 | **pluginapi**：实现 `AddAction/AddFilter` 注入，先不做复杂 i18n。 | 已新增 `pluginapi`，支持 `AddAction/AddFilter`、`T/N/X/XN`、`EscapeHTML`。 |
| done | 4 | **接入主题 functions**：让 `themeapi.Api.AddAction/AddFilter` 也注册到同一 registry，并记录来源 `theme:<name>`。 | 已接入 `themeapi.Api.AddAction/AddFilter`，启动时按“插件先、主题后”的顺序注册 hook。 |
| done | 5 | **模板函数**：增加 `pluginSlot/postContent/commentContent` 占位和请求级实现。 | 已在模板解析和请求级 clone 中接入；`postContent` 应用 `post.content_html`，`commentContent` 应用 `comment.render_html`。 |
| done | 6 | **插件资源路由**：实现 `/plugin-assets/*` 路径校验和静态输出。 | 已实现 `/plugin-assets/{plugin_id}/{path}`，只暴露已启用插件的资源，并做路径穿越校验。 |
| done | 7 | **组件声明 filter**：后台组件页和前台解析都接入 `widgets.available`。 | 已完成：后台组件列表和前台解析都会应用 `widgets.available`；`WidgetDecl/WidgetInfo/WidgetConfigItem` 已携带来源信息，支持保存 `plugin:<id>`。 |
| done | 8 | **插件组件渲染**：支持 plugin widget 的模板执行或 `widget.render`。 | 已完成：`renderWidgets` 会按 `Source=plugin` 分流到插件渲染器；插件组件支持 `widget.render` action 输出，未输出时回退到 `plugins/<id>/widgets/<widget_id>.gohtml` 模板；模板可用 `.tp`、`pluginOption`、`default/toInt`。 |
| done | 9 | **迁移 saying**：把 default 主题里的 saying 能力迁到 `plugins/saying`。 | 已完成：新增 `plugins/saying`，通过 manifest 声明组件和默认启用，组件模板由插件自带；default 主题已移除 saying 声明和模板。 |
| done | 10 | **评论表情**：实现 `comment.render_html` + `comment_form.after_textarea`，迁移 twentytwenty 表情。 | 已完成：新增 `plugins/post-comment-enhance`，通过 `comment.render_html` 渲染短码图片、通过 `comment_form.after_textarea` 输出表情面板，并将 twentytwenty 的主题私有实现迁出。 |
| done | 11 | **i18n 完整化**：插件文本域绑定、`.tp.T`、`pluginapi.Api.T/N/X/XN`。 | 已完成：`pluginapi.Api.T/N/X/XN`、插件文本域绑定、插件模板 `.tp.T` 都已接入。 |
| done | 12 | **后台插件页**：最小列表页展示启用状态、加载错误、hook/source 信息。 | 已完成：新增 `/admin/plugins` 插件列表、启用/停用/重载操作、hook/source/组件/设置展示、加载错误展示，并支持 `/admin/plugin/:id/settings` 配置插件全局选项。 |

当前最近一次验证：`go test ./...` 已通过。

### 20.9 最小测试清单

单元测试：

- `internal/plugin`: manifest ID 校验、路径校验、启用列表解析。
- `internal/plugin`: action/filter priority 顺序、panic recover、来源记录。
- `internal/render`: `pluginSlot/postContent/commentContent` 在请求级上下文下不串数据。
- `internal/theme`: `ResolveWidgets` 兼容旧配置，并能处理 plugin source。

集成/手验：

1. 启用 `saying` 插件，默认主题 sidebar 显示博主动态。
2. 切换到 twentytwenty/spread，只要主题调用 `renderWidgets`，仍能添加并显示 saying。
3. 禁用 `saying` 插件，前台跳过旧 saying 配置且不报错。
4. 启用评论表情插件，评论表单出现表情面板，提交后评论正文显示图片。
5. 切换语言后，插件输出的“博主动态 / 表情”等文案跟随翻译。
6. `go test ./...` 通过。

### 20.10 暂缓项

以下内容明确放到后续迭代：

- 插件 zip 上传安装和卸载清理。
- 插件版本升级迁移 hook。
- 插件路由注册。
- 插件市场/签名/自动更新。
- WASM 或更强沙箱。
- 更多 hook 点；只有出现明确插件需求并补文档后再加。

## 21. 参考资料

- [WordPress Plugin Basics](https://developer.wordpress.org/plugins/plugin-basics/)
- [WordPress Hooks](https://developer.wordpress.org/plugins/hooks/)
- [WordPress Activation / Deactivation Hooks](https://developer.wordpress.org/plugins/plugin-basics/activation-deactivation-hooks/)
- [WordPress Uninstall Methods](https://developer.wordpress.org/plugins/plugin-basics/uninstall-methods/)
