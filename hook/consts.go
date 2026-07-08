package hook

const (
	PriorityEarly   = 5
	PriorityDefault = 10
	PriorityLate    = 20
)

const (
	// ActionHeadEnd 在主题 <head> 结束前触发，扩展可写入样式、脚本或 meta。
	// 主题模板中应当写 {{do_action "head.end" .}} 才能触发调用
	ActionHeadEnd = "head.end"
	// ActionBodyEnd 在主题 </body> 前触发，扩展可写入延迟脚本。
	// 主题模板中应当写 {{do_action "body.end" .}} 才能触发调用
	ActionBodyEnd = "body.end"
	// ActionCommentFormAfterTextarea 在评论表单 textarea 后触发，扩展可写入表情、附件等控件。
	// 默认情况下, 主题会使用 {{comment_form .}} 输出评论表单, 这个模板函数内部会触发本 action
	// 如果主题不使用这个模板函数, 而是自行输出评论表单, 则应当写 {{do_action "comment.form.after_textarea" .}} 才能触发调用
	ActionCommentFormAfterTextarea = "comment.form.after_textarea"
	// ActionAdminPostFormAfterTextarea 在后台文章编辑表单 textarea 后触发，扩展可写入表情、附件等控件。
	// 后台文章编辑页面 {{- do_action "admin.post.form.after_textarea" $ -}} 调用
	ActionAdminPostFormAfterTextarea = "admin.post.form.after_textarea"
	// ActionWidgetRender 允许插件直接渲染自己的组件；未输出时回退到 widgets/<id>.gohtml。
	// 主题模板中 {{render_widgets "<area>" .}} 时会触发,
	// - 如果插件通过 AddAction 注册了该 action, 则会使用该 action 生成的 html 作为小组件
	// - 否则会回退到使用 widgets/<id>.gohtml 模板文件
	ActionWidgetRender = "widget.render"

	// FilterWidgetRenderHTML 过滤单个组件渲染后的 HTML。
	FilterWidgetRenderHTML = "widget.render_html"
	// FilterPostTitle 过滤文章/页面标题文本，运行于 post_title 模板函数内部。
	// 执行 post_title 模板函数时, 会应用这个 filter
	FilterPostTitle = "post.title"
	// FilterPostExcerptHTML 过滤列表摘要 HTML，运行于 post_excerpt 模板函数内部。
	// 执行 post_excerpt 模板函数时, 会应用这个 filter
	FilterPostExcerptHTML = "post.excerpt_html"
	// FilterPostContentHTML 过滤详情正文 HTML，运行于 post_content 模板函数内部。
	// 执行 post_content 模板函数时, 会应用这个 filter
	FilterPostContentHTML = "post.content_html"
	// FilterPostFooterHTML 在文章正文末尾追加 HTML（如版权声明），运行于 post_content 模板函数内部。
	// 执行 post_content 模板函数时, 会应用这个 filter
	FilterPostFooterHTML = "post.footer_html"
	// FilterCommentContentHTML 过滤评论正文 HTML，运行于 comment_content 模板函数内部。
	// 主题通过 {{list_comments .}} 模板函数输出评论列表, 这个模板函数内部会触发本 filter
	// 如果主题自行输出评论, 需要自行调用 {{apply_filter .Content "comment.content_html"}}
	FilterCommentContentHTML = "comment.content_html"
	// FilterHeadMeta 过滤 OpenGraph / Twitter Card meta 标签 HTML，运行于 headMeta 模板函数内部。
	// 插件可改写、追加或清空 meta 标签（返回空字符串即清除）。
	// 主题调用 {{head_meta . }} 时, 触发 head_meta 模板函数执行, 内部会应用这个 filter
	FilterHeadMeta = "head.meta"
)

// Consts 聚合所有 hook 名称常量和优先级值，供 yaegi 解释器一次性导出。
// 新增 hook 常量和优先级时只需修改此结构体，无需在 hook_exports.go 中逐个添加导出项。
var Consts = struct {
	ActionHeadEnd,
	ActionBodyEnd,
	ActionCommentFormAfterTextarea,
	ActionAdminPostFormAfterTextarea,
	ActionWidgetRender,
	FilterPostTitle,
	FilterPostExcerptHTML,
	FilterPostContentHTML,
	FilterPostFooterHTML,
	FilterCommentContentHTML,
	FilterWidgetRenderHTML,
	FilterHeadMeta string
	PriorityEarly,
	PriorityDefault,
	PriorityLate int
}{
	ActionHeadEnd:                    ActionHeadEnd,
	ActionBodyEnd:                    ActionBodyEnd,
	ActionCommentFormAfterTextarea:   ActionCommentFormAfterTextarea,
	ActionAdminPostFormAfterTextarea: ActionAdminPostFormAfterTextarea,
	ActionWidgetRender:               ActionWidgetRender,
	FilterPostTitle:                  FilterPostTitle,
	FilterPostExcerptHTML:            FilterPostExcerptHTML,
	FilterPostContentHTML:            FilterPostContentHTML,
	FilterPostFooterHTML:             FilterPostFooterHTML,
	FilterCommentContentHTML:         FilterCommentContentHTML,
	FilterWidgetRenderHTML:           FilterWidgetRenderHTML,
	FilterHeadMeta:                   FilterHeadMeta,
	PriorityEarly:                    PriorityEarly,
	PriorityDefault:                  PriorityDefault,
	PriorityLate:                     PriorityLate,
}
