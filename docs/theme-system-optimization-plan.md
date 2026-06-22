# 主题系统代码优化梳理与实施计划

> 本文以当前代码为事实依据，`docs/theme-system-*.md` 仅作为演进背景参考。
>
> 实施状态：已完成第一轮落地（P0 并发隔离、P1 启动流程拆分、P1 路径校验、P2 历史版本注释清理），并通过 `go test ./...`。

## 1. 当前结论

主题系统已经从早期“前台模板内置在 `web/templates`”演进为：启动时释放/扫描 `themes/`，由 `theme.Manager` 管理元数据、激活主题、主题模板、`functions.goyaegi`、组件区域、组件配置和主题选项。前台每个请求通过 `store.DataLoader` 全量缓存公开数据，再把常用数据注入模板，同时把 loader 放入当前模板渲染状态，供 `themeData` 调用。

主要可优化点不是“功能缺失”，而是**责任边界和并发模型不够清晰**：

1. `internal/render` 的当前模板、当前组件选项、当前 loader 已在模板执行期串行化，避免并发请求相互覆盖。
2. `theme.API` 不再由 `Public.base()` 提前写入共享 loader，而是在 `themeData` 调用期间按当前渲染请求临时绑定 loader。
3. 主题初始化、模板 provider 注入、运行时资源路由已从 `createWebHandler` 拆成独立辅助函数。
4. `docs/template-data-reference.md` 已更新为当前主题配置与模板数据参考。
5. 主要历史版本注释已改为当前能力描述。

## 2. 代码事实梳理

### 2.1 启动与主题加载

- `createWebHandler` 先创建模板渲染器，再调用 `ensureThemesOnDisk()` 把 `web.Themes` 中的内嵌主题释放到磁盘 `themes/`（已存在 `theme.yaml` 的主题会跳过），随后创建 `theme.Manager`：`cmd/server/web.go:124`、`cmd/server/web.go:145`、`cmd/server/web.go:146`、`cmd/server/web.go:276`。
- `Manager` 启动时扫描 `themesDir` 的子目录，并用 `LoadTheme(dir)` 读取 `theme.yaml`：`internal/theme/manager.go:40`、`internal/theme/manager.go:54`、`internal/theme/theme.go:75`。
- 当前主题由 Setting 表的 `current_theme` 决定，缺省回退为 `default`：`internal/theme/manager.go:20`、`internal/theme/manager.go:110`。
- 启动时调用 `tm.LoadTheme(context.Background(), "")` 加载当前主题模板与 functions 脚本，失败会自动回退默认主题并持久化 `current_theme=default`：`cmd/server/web.go:156`、`internal/theme/recovery.go:31`、`internal/theme/recovery.go:90`。

### 2.2 模板渲染与主题模板层级

- 主题模板加载由 `render.Renderer.LoadTheme(themeDir)` 完成：先解析主题模板，再补主题自定义组件模板、内置组件模板和后台/认证模板：`internal/render/render.go:126`。
- 前台页面按 `TemplateHierarchy` 查找模板：文章 `post.gohtml -> index.gohtml`，页面 `page.gohtml -> post.gohtml -> index.gohtml`，列表/归档/搜索有对应 fallback：`internal/render/render.go:157`、`internal/render/render.go:169`。
- `Public` 处理器渲染时通过 `ResolveTemplate(pageType)` 决定实际模板，并在预览主题时用 `PreviewInstance` 渲染预览模板：`internal/handler/public.go:64`、`internal/handler/public.go:328`、`internal/render/render.go:669`。
- 主题静态资源统一从 `/theme-assets/*filepath` 提供，管理员预览时优先走预览主题资源：`cmd/server/web.go:201`。

### 2.3 前台数据流

- 前台首页、搜索、动态路由、文章页、页面、分类、标签等入口都会调用 `st.LoadAllCached(c)`，并在内存中查询公开数据：`internal/handler/public.go:269`、`internal/handler/public.go:313`、`internal/handler/public.go:337`、`internal/handler/public.go:620`。
- `DataLoader` 一次加载 posts、comments、categories、tags、users、settings、post_categories、post_tags 等数据，并预构建索引：`internal/store/loader.go:15`、`internal/store/loader.go:36`。
- `Public.base()` 注入通用模板数据，并在有 loader 时把 loader 放入模板数据的内部 key，供渲染期 `themeData` 使用：`internal/handler/public.go:70`、`internal/handler/public.go:73`。
- 通用数据包括站点信息、菜单、当前用户、CSRF、近期文章、分类、标签、归档月份、近期评论、主题版本等：`internal/handler/public.go:100`、`internal/handler/public.go:115`、`internal/handler/public.go:132`。

### 2.4 ThemeAPI 与 functions.goyaegi

- `theme.API` 是给 `functions.goyaegi` 使用的只读视图层，暴露 `PostView`、`CategoryView`、`TagView`、`CommentView`、`UserView` 等结构，避免直接暴露 GORM model：`internal/theme/api.go:14`、`internal/theme/api.go:169`。
- `CompileFunctions` 优先读取 `functions.go`，其次读取 `functions.goyaegi`，用 yaegi 编译执行，并要求脚本定义 `Register()`：`internal/theme/functions.go:24`、`internal/theme/functions.go:65`、`internal/theme/functions.go:70`。
- 默认主题的 `functions.goyaegi` 注册了 `popular_posts` 和 `saying` 两个 provider：`web/themes/default/functions.goyaegi:8`。
- 模板通过 `themeData` 访问 provider，例如热门文章组件调用 `themeData "popular_posts"`：`web/themes/default/widgets/popular_posts.gohtml:1`。

### 2.5 组件与主题选项

- `theme.yaml` 当前包含元数据、`widget_areas`、`widgets`、组件级 `options`、主题全局 `options`：`internal/theme/theme.go:15`、`web/themes/default/theme.yaml:11`、`web/themes/default/theme.yaml:16`、`web/themes/default/theme.yaml:84`。
- 组件配置存在 Setting 表，key 为 `widget_<area>`；v6 新格式为 `[{"id":"...","opts":{...}}]`，`ResolveWidgets` 仍兼容 v4/v5 的字符串数组：`internal/theme/widgets.go:36`、`internal/theme/widgets.go:67`。
- 后台组件管理页按当前主题声明构建“可用组件”和“区域面板”，保存时写入 v6 JSON：`internal/handler/admin_widgets.go:32`、`internal/handler/admin_widgets.go:125`。
- 组件渲染通过 `renderWidgets` 遍历当前区域的 `WidgetInfo`，执行 `widget_<id>` 命名模板；组件选项通过 `widgetOption` 读取：`internal/render/render.go:371`、`internal/render/render.go:363`。
- 主题全局选项使用 `option_<theme>_<option>` 作为 Setting key，并通过 `themeData "option" "custom_css"` 或后台主题选项页读取：`internal/theme/options.go:5`、`internal/render/render.go:316`、`internal/handler/admin_theme_options.go:12`。

### 2.6 后台主题管理

- `/admin/themes` 支持上传、激活、删除、预览、截图：`cmd/server/routes.go:155`、`internal/handler/admin_theme.go:21`。
- `/admin/theme/files` 及相关 JSON 接口支持在线编辑主题文件，可编辑文件类型限制在模板、CSS/JS、yaml、翻译文件和 `functions.go/functions.goyaegi`：`cmd/server/routes.go:164`、`internal/handler/admin_theme_files.go:14`、`internal/theme/recovery.go:226`。
- 保存 `functions.go/functions.goyaegi` 后会触发当前主题重载；普通模板/css/yaml 保存不会自动重载所有场景：`internal/handler/admin_theme_files.go:107`。

## 3. 现有 docs 的可信度

| 文档 | 状态判断 | 说明 |
|---|---|---|
| `docs/theme-system-design.md` | 过期设计稿 | 仍描述早期 `pages.data/widgets` 方案，不应作为当前实现依据。 |
| `docs/theme-system-v2.md` | 部分有效 | 文件编辑器、yaegi、ThemeAPI、恢复机制仍有参考价值，但不是最终心智模型。 |
| `docs/theme-system-v3.md` | 部分有效 | 模板层级、全局数据、fragment 方向仍接近现状；但“去掉 Widget 概念”“theme.yaml 纯元数据”已被 v4-v6 推翻。 |
| `docs/theme-system-v4.md` | 部分有效 | 组件区域和 `renderWidgets` 的来源文档。 |
| `docs/theme-system-v5.md` | 部分有效 | 全局主题选项仍存在，但部分组件相关选项已迁到组件级 options。 |
| `docs/theme-system-v6.md` | 最接近现状 | 组件级选项、重复组件、v6 JSON 格式与当前代码最一致。 |
| `docs/template-data-reference.md` | 当前参考入口 | 已更新为当前主题配置、模板数据与函数参考；仍需随代码变更同步维护。 |

## 4. 优化目标

1. **让主题运行时状态显式化**：减少 `render` 包级全局变量，改为请求级或 Renderer 实例级状态。
2. **让 ThemeAPI 请求隔离**：每个请求使用独立的 API/loader 视图，避免并发请求相互覆盖。
3. **让启动流程可读**：把主题初始化、provider 注入、资源路由拆成独立函数。
4. **让文档只有一个“当前事实入口”**：保留历史设计稿，但明确当前以 AGENTS + template-data-reference + 本文/后续实施文档为准。
5. **降低历史版本噪音**：把代码注释中的 v2/v3/v5/v6 历史标签改为当前语义描述。

## 5. 建议的优化项

### P0：修正主题运行时并发模型

#### 问题

历史问题：`theme.Manager.SetLoaderForRequest(loader)` 会修改 `m.currentAPI.loader`，而 `currentAPI` 是当前主题共享实例。并发请求同时渲染模板时，后一个请求可能覆盖前一个请求的 loader，导致 `themeData` provider 读到错误请求的数据。

同时，`render` 包有多个全局变量：`themeDataProvider`、`optionProvider`、`themeWidgetsProvider`、`currentTemplate`、`currentWidgetOptions`：`internal/render/render.go:300`、`internal/render/render.go:308`、`internal/render/render.go:332`、`internal/render/render.go:347`、`internal/render/render.go:355`。其中 `currentWidgetOptions` 在 `renderWidgets` 循环中被设置/清空，跨请求并发时也可能互相污染。

#### 建议

1. 将 `theme.API` 拆为：
   - `ThemeRuntime`：编译后的 provider 注册表，不持有 loader。
   - `RequestAPI`：每次渲染绑定一个 loader，provider 执行时从请求上下文取数据。
2. 将 `themeData`、`themeWidgets`、`option`、`widgetOption` 做成 Renderer 实例级或模板执行上下文级函数，避免包级可变状态。
3. `renderWidgets` 不再依赖 `currentTemplate/currentWidgetOptions` 全局变量，改为自定义 render 实例持有模板和当前组件选项，或把组件数据包装进执行上下文。

#### 验收

- 增加并发渲染测试：两个请求使用不同 DataLoader/不同组件配置时，`themeData` 与 `widgetOption` 不串数据。
- `go test ./...` 通过。

#### 实施状态

- 已移除 `Public.base()` 对 `Manager.SetLoaderForRequest` 的依赖，改为把 loader 写入模板数据内部 key。
- 模板执行期用 `renderStateMu` 串行化 `currentTemplate`、`currentWidgetOptions`、`currentThemeLoader`，避免跨请求覆盖。
- `themeDataFunc` 在调用 provider 前从当前渲染状态取请求级 loader，通过 `API.CallProvider` 串行化临时绑定，调用结束后清空。
- 已恢复 `themeDataFunc` 的 1 秒超时控制；超时返回后 provider goroutine 如仍在执行，会继续持有 API 锁，避免与后续请求串 loader。
- 已新增 `TestThemeRenderStateIsRequestScoped` 并发测试。
- 已通过 `go test ./...`。

### P1：整理主题初始化与资源路由

#### 问题

`createWebHandler` 同时负责中间件、模板、静态资源、主题释放、Manager 初始化、provider 注入和 `/theme-assets` 路由，主题相关代码分散在 `cmd/server/web.go:143` 到 `cmd/server/web.go:222`。

#### 建议

新增私有辅助函数（仍在 `cmd/server` 内即可）：

- `initThemeManager(st, tplRenderer) (*theme.Manager, error)`：封装 `ensureThemesOnDisk`、`NewManager`、`SetRenderer`、默认主题 FS、启动加载。
- `registerThemeTemplateProviders(tm, st)`：封装 `SetThemeWidgetsProvider` 与 `SetOptionProvider`，后续 P0 后可迁移为实例级。
- `registerThemeAssetsRoute(r, tm)`：封装 `/theme-assets/*filepath`。

这样可以先改善可读性，不改变行为。

#### 实施状态

- 已新增 `initThemeManager`、`registerThemeTemplateProviders`、`registerThemeAssetsRoute`，`createWebHandler` 中主题相关主流程更清晰。
- 已通过 `go test ./...`。

### P1：统一主题当前值读取，减少重复查 Setting

#### 问题

`tm.Current(context.Background())` 在 widgets provider、option provider、theme assets 路由中都会读取 Setting 表：`cmd/server/web.go:173`、`cmd/server/web.go:183`、`cmd/server/web.go:205`。前台 `base()` 已经尽量从 DataLoader 读取当前主题名，但 provider 仍绕回 DB。

#### 建议

- 请求链路中统一解析 `ActiveTheme`，放入渲染上下文，provider/asset/template 共享。
- 对非请求场景（后台主题页）保留 `tm.Current(ctx)`。
- 如不立刻做 P0，可先在 `Public.base()` 注入 `CurrentTheme` / `ThemeVersion`，并在 `themeWidgetsProvider` 支持从当前请求上下文读取主题。

#### 实施状态

- 已在 `Public.base()` 中一次性解析当前/预览主题，并注入 `CurrentTheme`、`ThemeVersion` 和内部 `render.ThemeDataKey`。
- 模板执行期新增 `render.CurrentTheme()` 请求级状态，`themeWidgetsProvider` 和 `optionProvider` 优先复用当前渲染请求的主题，非模板渲染场景再回退 `tm.Current(context.Background())`。
- 已扩展 `TestThemeRenderStateIsRequestScoped`，同时覆盖请求级 loader 与当前主题不会跨并发渲染串数据。

### P1：修正 ThemeFilePath 路径校验

#### 问题

`ThemeFilePath` 使用 `strings.HasPrefix(fullPath, t.Dir)` 判断路径是否在主题目录内：`internal/theme/recovery.go:208`。如果存在同前缀目录（例如 `themes/default2`），纯字符串前缀判断不够严谨。

#### 建议

使用 `filepath.Abs` + `filepath.Rel` 校验，拒绝 `rel == ".."` 或以 `../` 开头的路径；同时确保传入 `relPath` 不是绝对路径。

#### 实施状态

- 已将 `ThemeFilePath` 改为 `Abs + Rel` 校验，并拒绝绝对路径和 `../` 穿越。
- 已新增 `TestThemeFilePathRejectsTraversalAndPrefixSibling`，覆盖同前缀兄弟目录逃逸场景。
- 已通过 `go test ./...`。

### P1：主题 zip 安装安全增强

#### 问题

主题上传限制了总 zip 文件大小 5MB，并做路径穿越和扩展名白名单：`internal/handler/admin_theme.go:19`、`internal/handler/admin_theme.go:221`。但解压后总大小、文件数量、单文件大小没有统一上限；安装时 `copyDir` 会先删除目标目录：`internal/theme/manager.go:191`。

#### 建议

- 解压时限制文件数量、解压后总字节数、单文件字节数。
- 安装覆盖已有主题前先复制到临时目录并校验，通过后原子替换或至少先备份旧目录。
- 在线编辑和 zip 上传共享同一份“允许主题文件类型”判断，避免白名单分叉。

#### 实施状态

- 已为 zip 解压增加文件数量、单文件大小、解压后总大小、文件名长度限制。
- zip 解压路径校验已改为 `safeThemeZipPath` + `pathWithinDir`，不再依赖简单字符串前缀判断。
- 主题安装已改为“暂存复制 → 暂存校验 → 备份旧目录 → `os.Rename` 替换 → 清理备份”的流程，替代安装前直接删除目标目录。
- 已新增 `TestManagerInstallReplacesThemeAtomically`，覆盖覆盖安装与临时目录清理。

### P2：拆分主题包职责

#### 问题

`internal/theme` 同时负责 yaml 元数据、Manager 生命周期、ThemeAPI、yaegi、组件、选项、恢复、文件编辑安全路径。代码文件数量不多，但概念密度很高。

#### 建议

保持包名不变，按职责重命名/拆分文件并统一注释：

- `manifest.go`：`Theme`、`WidgetArea`、`WidgetDecl`、`OptionDecl`、`LoadTheme`。
- `manager.go`：扫描、安装、删除、激活、当前主题。
- `runtime.go`：加载模板、编译 functions、恢复信息、预览主题。
- `api.go`：只读视图和 ThemeAPI。
- `widgets.go` / `options.go`：组件配置和主题选项。

这属于可读性整理，风险低，但建议在 P0/P1 后做。

#### 实施状态

- 已先做低风险拆分：新增 `runtime.go` 承载主题运行时加载、fallback、预览模板缓存和恢复信息逻辑。
- `recovery.go` 收敛为主题目录与文件编辑安全相关逻辑，降低单文件概念密度。
- `api.go`、`widgets.go`、`options.go` 保持按只读 API、组件配置、主题选项分工。

### P2：把历史版本注释改成当前语义

#### 问题

代码中存在“v2 新增字段”“v3: 所有常用数据始终注入”“主题文件编辑器 (v2)”“主题选项 (v5)”等注释：`internal/theme/manager.go:31`、`internal/handler/public.go:70`、`cmd/server/routes.go:164`、`cmd/server/routes.go:184`。这些对当前维护者帮助有限，反而需要回忆历史版本。

#### 建议

把历史版本注释改成能力描述，例如：

- “主题运行时状态：模板渲染器、functions 脚本、恢复信息”。
- “前台模板通用数据始终注入，主题无需声明 data 依赖”。
- “主题文件编辑器”。
- “主题全局选项”。

#### 实施状态

- 已清理主要 Go 代码中的 `v2/v3/v5/v6` 历史版本注释，保留必要的兼容格式说明并改为“旧格式/对象数组格式”。

### P2：文档收敛

#### 建议

1. `AGENTS.md` 增加主题系统当前架构、修改风险点、优先阅读路径。
2. `docs/template-data-reference.md` 更新为当前 v6 主题开发参考。
3. 在历史设计稿开头加一句“历史方案，当前实现请看 AGENTS/template-data-reference/theme-system-optimization-plan”。这一步可选，避免一次性改很多历史文档。

#### 实施状态

- 已更新 `AGENTS.md` 的主题系统当前模型说明。
- 已更新 `docs/template-data-reference.md` 为当前主题配置、模板数据与函数参考。
- 已在 `theme-system-design.md` 与 `theme-system-v2.md` 到 `theme-system-v6.md` 开头补充历史设计稿提示，明确当前实现入口。

## 6. 建议实施顺序

1. **文档先行（当前任务）**：更新 AGENTS 与模板数据参考，新增本文。
2. **P0 并发隔离**：消除 API loader 和 widgetOption 的全局状态风险，补测试。
3. **P1 启动流程拆分**：把 `cmd/server/web.go` 主题相关逻辑抽成小函数，降低理解成本。
4. **P1 安全小修**：路径校验、zip 解压限制、主题安装覆盖策略。
5. **P2 注释和包内整理**：删除历史版本噪音，按职责重排文件。
6. **P2 文档收敛**：视需要给历史方案加过期提示。

## 7. 后续实现时的测试建议

- `go test ./...` 作为最低线。
- 新增 `internal/render` 或 `internal/theme` 并发测试，覆盖并发 `themeData` / `renderWidgets`。
- 浏览器手验：默认主题、single 主题、主题预览、组件保存、主题选项保存、主题文件保存后重载。
- 如果改 `/theme-assets`：验证正常主题资源、预览主题资源、非法路径、无 assets 主题的 404。
