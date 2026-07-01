package render

import (
	"fmt"
	"html"
	"html/template"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/wxr"
)

// hookInvoke 在模板中使用 {{hookInvoke "funcName" "argName" argValue}}
// 其中 funcName 是在 functions.go 中通过 [RegisterFunc] 注册的函数
func hookInvoke(runtime *TemplateRuntime, ctx *RequestContext, name string, args ...any) any {
	if runtime == nil {
		return nil
	}
	provider := runtime.current().HookInvoke
	if provider == nil {
		return nil
	}
	return provider(ctx, name, args...)
}

// themeOption 在模板中使用 {{themeOption "<optionID>"}}
func themeOption(runtime *TemplateRuntime, ctx *RequestContext, optionID string) string {
	if runtime == nil {
		return ""
	}
	provider := runtime.current().ThemeOption
	if provider == nil {
		return ""
	}
	return provider(ctx, optionID)
}

// widgetOption 模板函数：读取当前渲染组件的选项值。
// themeOption 在模板中使用 {{widgetOption "<key>"}}
func widgetOption(ctx *RequestContext, key string) string {
	if ctx == nil || ctx.WidgetOptions == nil {
		return ""
	}
	return ctx.WidgetOptions[key]
}

func postTitle(runtime *TemplateRuntime, ctx *RequestContext, post any) template.HTML {
	title := reflectStringField(post, "Title")
	if h := hooks(runtime); h != nil {
		title = stringFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.HookPostTitle, title, post), title)
	}
	title = html.EscapeString(title)
	if title == "" {
		return ""
	}
	if isDraft(ctx) {
		title += ` <span class="draft-badge">` + translate(ctx, "草稿预览") + `</span>`
	}
	return template.HTML(`<h1 class="post-title">` + title + `</h1>`)
}

func translate(ctx *RequestContext, msg string) string {
	if ctx != nil {
		if tr, ok := dataValue(ctx.Data, "th").(interface{ T(string, ...any) string }); ok && tr != nil {
			return html.EscapeString(tr.T(msg))
		}
	}
	return html.EscapeString(msg)
}

func stringFromFilterValue(v any, fallback string) string {
	switch val := v.(type) {
	case string:
		return val
	case template.HTML:
		return string(val)
	case []byte:
		return string(val)
	default:
		return fallback
	}
}

func postExcerpt(runtime *TemplateRuntime, ctx *RequestContext, post any) template.HTML {
	html := postExcerptHTMLAny(post)
	if h := hooks(runtime); h != nil {
		html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.HookPostExcerptHTML, string(html), post))
	}
	return html
}

func postExcerptHTMLAny(post any) template.HTML {
	switch p := post.(type) {
	case *model.Post:
		if p == nil {
			return ""
		}
		return postExcerptHTML(p)
	case model.Post:
		return postExcerptHTML(&p)
	default:
		content := reflectStringField(post, "Content")
		excerpt := reflectStringField(post, "Excerpt")
		above, hasMore := wxr.SplitMore(content)
		switch {
		case hasMore:
			return renderContentHTML(above)
		case excerpt != "":
			return renderContentHTML(excerpt)
		case content != "":
			return renderContentHTML(content)
		default:
			return ""
		}
	}
}

// postContent 输出文章正文，并应用 post.content_html filter。
func postContent(runtime *TemplateRuntime, ctx *RequestContext, post any) template.HTML {
	html := postContentHTML(post)
	if h := hooks(runtime); h != nil {
		html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.HookPostContentHTML, string(html), post))
	}
	return html
}

func postContentHTML(post any) template.HTML {
	switch p := post.(type) {
	case *model.Post:
		if p == nil {
			return ""
		}
		return detailHTML(p)
	case model.Post:
		return detailHTML(&p)
	default:
		content := reflectStringField(post, "Content")
		id := reflectUintField(post, "ID")
		if content == "" {
			return ""
		}
		return renderContentHTML(wxr.RenderDetail(content, id))
	}
}

// commentContent 输出评论正文，并应用 comment.render_html filter。
func commentContent(runtime *TemplateRuntime, ctx *RequestContext, comment any) template.HTML {
	html := template.HTML(html.EscapeString(reflectStringField(comment, "Content")))
	if h := hooks(runtime); h != nil {
		html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.HookCommentContentHTML, string(html), comment))
	}
	return html
}

func htmlFromFilterValue(v any) template.HTML {
	switch val := v.(type) {
	case template.HTML:
		return val
	case string:
		return template.HTML(val)
	case []byte:
		return template.HTML(string(val))
	default:
		return ""
	}
}

func postTags(post any) template.HTML {
	tags := reflectSliceField(post, "Tags")
	if len(tags) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<div class="post-tags">`)
	for _, tag := range tags {
		name := html.EscapeString(reflectStringField(tag, "Name"))
		slug := reflectStringField(tag, "Slug")
		if name == "" || slug == "" {
			continue
		}
		out.WriteString(`<a class="tag" href="`)
		out.WriteString(html.EscapeString(permalink.Tag(slug)))
		out.WriteString(`">#`)
		out.WriteString(name)
		out.WriteString(`</a>`)
	}
	out.WriteString(`</div>`)
	return template.HTML(out.String())
}

func postNavigation(ctx *RequestContext, data any, classes ...string) template.HTML {
	if data == nil && ctx != nil {
		data = ctx.Data
	}
	prev := dataValue(data, "PrevPost")
	next := dataValue(data, "NextPost")
	if isNilAny(prev) && isNilAny(next) {
		return ""
	}
	class := "post-nav"
	if len(classes) > 0 && strings.TrimSpace(classes[0]) != "" {
		class = strings.TrimSpace(classes[0])
	}
	var out strings.Builder
	out.WriteString(`<nav class="`)
	out.WriteString(html.EscapeString(class))
	out.WriteString(`">`)
	out.WriteString(`<span class="prev">`)
	if !isNilAny(prev) {
		out.WriteString(`<a href="`)
		out.WriteString(html.EscapeString(postURL(prev)))
		out.WriteString(`">← `)
		out.WriteString(html.EscapeString(reflectStringField(prev, "Title")))
		out.WriteString(`</a>`)
	}
	out.WriteString(`</span><span class="next">`)
	if !isNilAny(next) {
		out.WriteString(`<a href="`)
		out.WriteString(html.EscapeString(postURL(next)))
		out.WriteString(`">`)
		out.WriteString(html.EscapeString(reflectStringField(next, "Title")))
		out.WriteString(` →</a>`)
	}
	out.WriteString(`</span></nav>`)
	return template.HTML(out.String())
}

func isNilAny(v any) bool {
	if v == nil {
		return true
	}
	// 模板数据通过 any 传递时，(*model.Post)(nil) 这类 typed nil 会被装进非 nil 的 interface。
	// 这里保留极小范围的反射，只用于识别 Go 运行时可为 nil 的类型，避免文章导航等模板辅助函数把 typed nil 当成有效数据渲染。
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func renderMenu(ctx *RequestContext, location string, data ...any) template.HTML {
	source := any(nil)
	if len(data) > 0 {
		source = data[0]
	}
	if source == nil && ctx != nil {
		source = ctx.Data
	}
	items := menuItemsForLocation(source, location)
	if len(items) == 0 {
		return ""
	}
	class := "menu"
	if location != "" {
		class += " menu-" + slugClass(location)
	}
	var out strings.Builder
	out.WriteString(`<nav class="`)
	out.WriteString(html.EscapeString(class))
	out.WriteString(`">`)
	out.WriteString(`<ul class="menu-items">`)
	for _, item := range items {
		title := html.EscapeString(reflectStringField(item, "Title"))
		url := reflectStringField(item, "URL")
		if url == "" {
			url = postURL(item)
		}
		if title == "" || url == "" {
			continue
		}
		target := reflectStringField(item, "Target")
		out.WriteString(`<li class="menu-item"><a href="`)
		out.WriteString(html.EscapeString(url))
		out.WriteString(`"`)
		if target != "" {
			out.WriteString(` target="`)
			out.WriteString(html.EscapeString(target))
			out.WriteString(`" rel="noreferrer"`)
		}
		out.WriteString(`>`)
		out.WriteString(title)
		out.WriteString(`</a>`)
		children := reflectSliceField(item, "Children")
		if len(children) > 0 {
			out.WriteString(`<ul class="sub-menu">`)
			for _, child := range children {
				writeMenuItem(&out, child)
			}
			out.WriteString(`</ul>`)
		}
		out.WriteString(`</li>`)
	}
	out.WriteString(`</ul></nav>`)
	return template.HTML(out.String())
}

func writeMenuItem(out *strings.Builder, item any) {
	title := html.EscapeString(reflectStringField(item, "Title"))
	url := reflectStringField(item, "URL")
	if title == "" || url == "" {
		return
	}
	target := reflectStringField(item, "Target")
	out.WriteString(`<li class="menu-item"><a href="`)
	out.WriteString(html.EscapeString(url))
	out.WriteString(`"`)
	if target != "" {
		out.WriteString(` target="`)
		out.WriteString(html.EscapeString(target))
		out.WriteString(`" rel="noreferrer"`)
	}
	out.WriteString(`>`)
	out.WriteString(title)
	out.WriteString(`</a>`)
	children := reflectSliceField(item, "Children")
	if len(children) > 0 {
		out.WriteString(`<ul class="sub-menu">`)
		for _, child := range children {
			writeMenuItem(out, child)
		}
		out.WriteString(`</ul>`)
	}
	out.WriteString(`</li>`)
}

func menuItemsForLocation(data any, location string) []any {
	if location != "" {
		if menus := dataValue(data, "Menus"); menus != nil {
			if items := menuLocationItems(menus, location); len(items) > 0 {
				return items
			}
		}
	}
	return anySlice(dataValue(data, "Menu"))
}

func menuLocationItems(menus any, location string) []any {
	switch m := menus.(type) {
	case map[string]any:
		return anySlice(m[location])
	default:
		return nil
	}
}

func anySlice(v any) []any {
	switch items := v.(type) {
	case []any:
		return items
	case []model.Post:
		out := make([]any, 0, len(items))
		for i := range items {
			out = append(out, items[i])
		}
		return out
	case []hook.PostView:
		out := make([]any, 0, len(items))
		for i := range items {
			out = append(out, items[i])
		}
		return out
	default:
		return nil
	}
}

func bodyClass(data any) string {
	classes := []string{"site"}
	if pageType, _ := dataValue(data, "PageType").(string); pageType != "" {
		classes = append(classes, "page-type-"+slugClass(pageType))
	}
	if dataValue(data, "Post") != nil {
		classes = append(classes, "singular")
	}
	if dataValue(data, "List") != nil {
		classes = append(classes, "archive-list")
	}
	if uid, _ := dataValue(data, "CurrentUserID").(uint); uid != 0 {
		classes = append(classes, "logged-in")
	}
	if preview, _ := dataValue(data, "IsPreview").(bool); preview {
		classes = append(classes, "theme-preview")
	}
	return strings.Join(uniqueClasses(classes), " ")
}

func postClass(post any, extra ...string) string {
	classes := append([]string{"post"}, extra...)
	if id := reflectUintField(post, "ID"); id > 0 {
		classes = append(classes, fmt.Sprintf("post-%d", id))
	}
	if postType := reflectStringField(post, "PostType"); postType != "" {
		classes = append(classes, "type-"+slugClass(postType))
	}
	if status := reflectStringField(post, "Status"); status != "" {
		classes = append(classes, "status-"+slugClass(status))
	}
	for _, cat := range reflectSliceField(post, "Categories") {
		if slug := reflectStringField(cat, "Slug"); slug != "" {
			classes = append(classes, "category-"+slugClass(slug))
		}
	}
	for _, tag := range reflectSliceField(post, "Tags") {
		if slug := reflectStringField(tag, "Slug"); slug != "" {
			classes = append(classes, "tag-"+slugClass(slug))
		}
	}
	return strings.Join(uniqueClasses(classes), " ")
}

func commentClass(comment any, extra ...string) string {
	classes := append([]string{"comment"}, extra...)
	if id := reflectUintField(comment, "ID"); id > 0 {
		classes = append(classes, fmt.Sprintf("comment-%d", id))
	}
	if parentID := reflectUintField(comment, "ParentID"); parentID > 0 {
		classes = append(classes, "comment-reply")
	} else {
		classes = append(classes, "comment-root")
	}
	if status := reflectStringField(comment, "Status"); status != "" {
		classes = append(classes, "status-"+slugClass(status))
	}
	return strings.Join(uniqueClasses(classes), " ")
}

func slugClass(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			lastDash = r == '-'
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func uniqueClasses(classes []string) []string {
	seen := make(map[string]bool, len(classes))
	out := make([]string, 0, len(classes))
	for _, class := range classes {
		class = strings.TrimSpace(class)
		if class == "" || seen[class] {
			continue
		}
		seen[class] = true
		out = append(out, class)
	}
	return out
}

// headMeta 生成 OpenGraph / Twitter Card meta 标签。
// 在模板 <head> 中使用 {{headMeta .}} 调用。
// 生成的 HTML 会经过 head.meta filter，插件可改写、追加或清空 meta 标签。
func headMeta(runtime *TemplateRuntime, ctx *RequestContext, data any) template.HTML {
	title := stringValue(data, "Title")
	desc := stringValue(data, "Description")
	url := stringValue(data, "CanonicalURL")
	siteName := stringValue(data, "SiteName")
	siteLogo := stringValue(data, "SiteLogo")

	if title == "" {
		return ""
	}

	var b strings.Builder
	writeMeta(&b, "og:title", title)
	if desc != "" {
		writeMeta(&b, "og:description", desc)
	}
	if url != "" {
		writeMeta(&b, "og:url", url)
	}
	ogType := "website"
	if dataValue(data, "Post") != nil {
		ogType = "article"
	}
	writeMeta(&b, "og:type", ogType)
	if siteName != "" {
		writeMeta(&b, "og:site_name", siteName)
	}
	if siteLogo != "" {
		writeMeta(&b, "og:image", siteLogo)
	}

	card := "summary"
	if siteLogo != "" {
		card = "summary_large_image"
	}
	writeMetaName(&b, "twitter:card", card)
	writeMetaName(&b, "twitter:title", title)
	if desc != "" {
		writeMetaName(&b, "twitter:description", desc)
	}
	if siteLogo != "" {
		writeMetaName(&b, "twitter:image", siteLogo)
	}

	html := template.HTML(b.String())
	if h := hooks(runtime); h != nil {
		html = htmlFromFilterValue(h.ApplyFilters(requestContext(ctx), hook.HookHeadMeta, string(html), data))
	}
	return html
}

func writeMeta(b *strings.Builder, property, content string) {
	b.WriteString(`<meta property="`)
	b.WriteString(html.EscapeString(property))
	b.WriteString(`" content="`)
	b.WriteString(html.EscapeString(content))
	b.WriteString(`">`)
	b.WriteByte('\n')
}

func writeMetaName(b *strings.Builder, name, content string) {
	b.WriteString(`<meta name="`)
	b.WriteString(html.EscapeString(name))
	b.WriteString(`" content="`)
	b.WriteString(html.EscapeString(content))
	b.WriteString(`">`)
	b.WriteByte('\n')
}

func stringValue(data any, key string) string {
	v := dataValue(data, key)
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func isDraft(ctx *RequestContext) bool {
	if ctx == nil {
		return false
	}
	value, _ := dataValue(ctx.Data, "IsDraft").(bool)
	return value
}

// ========== listComments 模板函数 ==========

// listComments 渲染评论列表和分页。
// 模板调用: {{listComments .}} 或 {{listComments . "ol" 44 "my_comment"}}
// 参数: args[0]=data(必填), args[1]=style(默认"ul"), args[2]=avatarSize(默认40), args[3]=callback模板名(默认"comment_item")
func listComments(runtime *TemplateRuntime, ctx *RequestContext, args ...any) template.HTML {
	if len(args) == 0 {
		return ""
	}
	data := args[0]
	if data == nil && ctx != nil {
		data = ctx.Data
	}
	if data == nil {
		return ""
	}

	// 解析可选参数
	style := "ul"
	avatarSize := 40
	callbackTpl := "comment_item"
	if len(args) > 1 {
		if s, ok := args[1].(string); ok && s != "" {
			style = s
		}
	}
	if len(args) > 2 {
		switch v := args[2].(type) {
		case int:
			if v > 0 {
				avatarSize = v
			}
		case int64:
			if v > 0 {
				avatarSize = int(v)
			}
		case float64:
			if v > 0 {
				avatarSize = int(v)
			}
		}
	}
	if len(args) > 3 {
		if s, ok := args[3].(string); ok && s != "" {
			callbackTpl = s
		}
	}

	// 提取模板数据字段
	commentsAny := dataValue(data, "Comments")
	defaultAvatar, _ := dataValue(data, "DefaultAvatar").(string)

	// 评论列表
	var comments []any
	switch c := commentsAny.(type) {
	case []model.Comment:
		comments = make([]any, len(c))
		for i := range c {
			comments[i] = c[i]
		}
	case []any:
		comments = c
	default:
		comments = reflectSliceField(data, "Comments")
	}

	// 检查主题是否定义了自定义评论模板
	hasCustomTpl := ctx != nil && ctx.Template != nil && ctx.Template.Lookup(callbackTpl) != nil

	var out strings.Builder

	// 列表标签
	listTag := style
	if listTag != "ol" && listTag != "ul" && listTag != "div" {
		listTag = "ul"
	}
	out.WriteString("<")
	out.WriteString(listTag)
	out.WriteString(` class="comment-list">`)

	for _, c := range comments {
		parentID := reflectUintField(c, "ParentID")
		if parentID != 0 {
			continue
		}
		writeCommentItem(&out, runtime, ctx, data, c, comments, callbackTpl, hasCustomTpl, avatarSize, defaultAvatar, listTag)
	}

	out.WriteString("</")
	out.WriteString(listTag)
	out.WriteString(">")

	return template.HTML(out.String())
}

// commentForm 渲染登录提示和评论表单（或"评论已关闭"提示）。
// 模板调用: {{commentForm .}}
func commentForm(runtime *TemplateRuntime, ctx *RequestContext, data any) template.HTML {
	if data == nil && ctx != nil {
		data = ctx.Data
	}
	if data == nil {
		return ""
	}

	commentOpen, _ := dataValue(data, "CommentOpen").(bool)
	mailEnabled, _ := dataValue(data, "MailEnabled").(bool)
	csrfToken, _ := dataValue(data, "CSRFToken").(string)
	postID := reflectUintField(dataValue(data, "Post"), "ID")

	var out strings.Builder

	// 登录提示
	writeCommentLoginTip(&out, ctx, data, csrfToken)

	// 评论表单或关闭提示
	if commentOpen {
		writeCommentForm(&out, runtime, ctx, data, postID, csrfToken, mailEnabled)
	} else {
		out.WriteString(`<p class="comment-closed">`)
		out.WriteString(ctx.T("评论已关闭"))
		out.WriteString(`</p>`)
	}

	return template.HTML(out.String())
}

// writeCommentItem 渲染单条评论（含 <li> 包裹）及其子回复。
func writeCommentItem(out *strings.Builder, runtime *TemplateRuntime, ctx *RequestContext,
	data any, comment any, allComments []any, callbackTpl string, hasCustomTpl bool,
	avatarSize int, defaultAvatar string, listTag string) {

	id := reflectUintField(comment, "ID")
	status := reflectStringField(comment, "Status")

	classes := "comment"
	if status == "pending" {
		classes += " comment-pending"
	}

	out.WriteString(`<li class="`)
	out.WriteString(html.EscapeString(classes))
	out.WriteString(`" id="comment-`)
	out.WriteString(strconv.FormatUint(uint64(id), 10))
	out.WriteString(`">`)

	if hasCustomTpl {
		itemData := commentToMap(comment)
		itemData["DefaultAvatar"] = defaultAvatar
		if th := dataValue(data, "th"); th != nil {
			itemData["th"] = th
		}
		var buf strings.Builder
		if err := ctx.Template.ExecuteTemplate(&buf, callbackTpl, itemData); err == nil {
			out.WriteString(buf.String())
		}
	} else {
		writeDefaultCommentItem(out, runtime, ctx, data, comment, avatarSize, defaultAvatar)
	}

	// 子回复
	writeCommentChildren(out, runtime, ctx, data, comment, allComments, callbackTpl, hasCustomTpl, avatarSize, defaultAvatar, listTag)

	out.WriteString(`</li>`)
}

// writeDefaultCommentItem 生成默认评论 HTML（与 default 主题结构一致）。
func writeDefaultCommentItem(out *strings.Builder, runtime *TemplateRuntime, ctx *RequestContext,
	data any, comment any, avatarSize int, defaultAvatar string) {

	id := reflectUintField(comment, "ID")
	author := html.EscapeString(reflectStringField(comment, "Author"))
	email := reflectStringField(comment, "Email")
	status := reflectStringField(comment, "Status")
	commenterRole := reflectStringField(comment, "CommenterRole")
	notifyOnReply := reflectBoolField(comment, "NotifyOnReply")
	createdAt := reflectTimeField(comment, "CreatedAt")
	parentID := reflectUintField(comment, "ParentID")
	replyToAuthor := reflectStringField(comment, "ReplyToAuthor")
	replyToID := reflectUintField(comment, "ReplyToID")

	sizeStr := strconv.Itoa(avatarSize)

	// comment-head
	out.WriteString(`<div class="comment-head">`)
	// avatar
	out.WriteString(`<img class="avatar" src="`)
	out.WriteString(html.EscapeString(avatarURL(email, defaultAvatar)))
	out.WriteString(`" alt="" width="`)
	out.WriteString(sizeStr)
	out.WriteString(`" height="`)
	out.WriteString(sizeStr)
	out.WriteString(`">`)
	// comment-id
	out.WriteString(`<a class="comment-id" href="#comment-`)
	out.WriteString(strconv.FormatUint(uint64(id), 10))
	out.WriteString(`">#`)
	out.WriteString(strconv.FormatUint(uint64(id), 10))
	out.WriteString(`</a>`)
	// author
	out.WriteString(`<span class="comment-author">`)
	out.WriteString(author)
	out.WriteString(`</span>`)
	// badges
	if commenterRole != "" {
		roleTitle := ""
		switch commenterRole {
		case "author":
			roleTitle = ctx.T("文章作者")
		case "admin":
			roleTitle = ctx.T("管理员")
		default:
			roleTitle = ctx.T("登录用户")
		}
		out.WriteString(`<span class="comment-badge comment-badge-`)
		out.WriteString(html.EscapeString(commenterRole))
		out.WriteString(`" title="`)
		out.WriteString(roleTitle)
		out.WriteString(`"></span>`)
	}
	if notifyOnReply {
		out.WriteString(`<span class="comment-badge comment-badge-notify" title="`)
		out.WriteString(ctx.T("回复时邮件通知"))
		out.WriteString(`"></span>`)
	}
	// time
	out.WriteString(`<time>`)
	if !createdAt.IsZero() {
		out.WriteString(html.EscapeString(createdAt.Format("2006-01-02 15:04")))
	}
	out.WriteString(`</time>`)
	// pending badge
	if status == "pending" {
		out.WriteString(`<span class="comment-pending-badge">`)
		out.WriteString(ctx.T("评论审核中,当前仅自己可见"))
		out.WriteString(`</span>`)
	}
	out.WriteString(`</div>`)

	// comment-body
	out.WriteString(`<div class="comment-body">`)
	if parentID != 0 && replyToAuthor != "" && replyToID > 0 {
		out.WriteString(`<a class="comment-reply-to" href="#comment-`)
		out.WriteString(strconv.FormatUint(uint64(replyToID), 10))
		out.WriteString(`">`)
		out.WriteString(html.EscapeString(ctx.T("回复 @%s", replyToAuthor)))
		out.WriteString(`</a>`)
	}
	out.WriteString(string(commentContent(runtime, ctx, comment)))
	out.WriteString(`</div>`)

	// reply button
	out.WriteString(`<button class="comment-reply" type="button" data-reply="`)
	out.WriteString(strconv.FormatUint(uint64(id), 10))
	out.WriteString(`" data-reply-target="comment-`)
	out.WriteString(strconv.FormatUint(uint64(id), 10))
	out.WriteString(`">`)
	out.WriteString(ctx.T("回复"))
	out.WriteString(`</button>`)
}

// writeCommentChildren 渲染评论的子回复列表。
func writeCommentChildren(out *strings.Builder, runtime *TemplateRuntime, ctx *RequestContext,
	data any, parent any, allComments []any, callbackTpl string, hasCustomTpl bool,
	avatarSize int, defaultAvatar string, listTag string) {

	parentID := reflectUintField(parent, "ID")
	var children []any
	for _, c := range allComments {
		if reflectUintField(c, "ParentID") == parentID {
			children = append(children, c)
		}
	}
	if len(children) == 0 {
		return
	}

	childrenTag := "ul"
	switch listTag {
case "ol":
		childrenTag = "ol"
	case "div":
		childrenTag = "div"
	}

	out.WriteString("<")
	out.WriteString(childrenTag)
	out.WriteString(` class="comment-children">`)

	for _, child := range children {
		writeCommentItem(out, runtime, ctx, data, child, allComments, callbackTpl, hasCustomTpl, avatarSize, defaultAvatar, listTag)
	}

	out.WriteString("</")
	out.WriteString(childrenTag)
	out.WriteString(">")
}

// commentsPagination 渲染评论分页导航。
// 模板调用: {{the_comments_pagination .}}
func commentsPagination(ctx *RequestContext, data any) template.HTML {
	var out strings.Builder
	writeCommentPagination(&out, ctx, data)
	return template.HTML(out.String())
}

// writeCommentPagination 渲染评论分页导航。
func writeCommentPagination(out *strings.Builder, ctx *RequestContext, data any) {
	pagerAny := dataValue(data, "CommentPager")
	if pagerAny == nil {
		return
	}
	pager, ok := pagerAny.(map[string]any)
	if !ok {
		if gh, ok2 := pagerAny.(gin.H); ok2 {
			pager = map[string]any(gh)
		} else {
			return
		}
	}
	pages, _ := toInt(pager["Pages"])
	if pages <= 1 {
		return
	}
	page, _ := toInt(pager["Page"])
	baseURL, _ := pager["BaseURL"].(string)

	out.WriteString(`<nav class="pagination comment-pagination">`)
	if page > 1 {
		out.WriteString(`<a href="`)
		out.WriteString(html.EscapeString(baseURL))
		out.WriteString(`?cpage=`)
		out.WriteString(strconv.Itoa(page - 1))
		out.WriteString(`#comments" data-cpage="`)
		out.WriteString(strconv.Itoa(page - 1))
		out.WriteString(`">`)
		out.WriteString(ctx.T("上一页"))
		out.WriteString(`</a>`)
	}
	out.WriteString(`<span class="page-info">`)
	out.WriteString(strconv.Itoa(page))
	out.WriteString(` / `)
	out.WriteString(strconv.Itoa(pages))
	out.WriteString(`</span>`)
	if page < pages {
		out.WriteString(`<a href="`)
		out.WriteString(html.EscapeString(baseURL))
		out.WriteString(`?cpage=`)
		out.WriteString(strconv.Itoa(page + 1))
		out.WriteString(`#comments" data-cpage="`)
		out.WriteString(strconv.Itoa(page + 1))
		out.WriteString(`">`)
		out.WriteString(ctx.T("下一页"))
		out.WriteString(`</a>`)
	}
	out.WriteString(`</nav>`)
}

// writeCommentLoginTip 渲染已登录用户的身份提示。
func writeCommentLoginTip(out *strings.Builder, ctx *RequestContext, data any, csrfToken string) {
	currentUser := dataValue(data, "CurrentUser")
	if isNilAny(currentUser) {
		return
	}
	displayName := reflectStringField(currentUser, "DisplayName")
	username := reflectStringField(currentUser, "Username")
	name := displayName
	if name == "" {
		name = username
	}
	if name == "" {
		return
	}

	out.WriteString(`<div class="comment-login-tip">`)
	// 使用 th.T 格式化（含 HTML 标签）
	if th := dataValue(data, "th"); th != nil {
		if tr, ok := th.(interface{ T(string, ...any) string }); ok {
			out.WriteString(tr.T("以 <strong>%s</strong> 的身份发表评论，", html.EscapeString(name)))
		}
	}
	out.WriteString(`<form class="inline" method="post" action="/auth/logout">`)
	if csrfToken != "" {
		out.WriteString(`<input type="hidden" name="_csrf" value="`)
		out.WriteString(html.EscapeString(csrfToken))
		out.WriteString(`">`)
	}
	out.WriteString(`<button type="submit">`)
	out.WriteString(ctx.T("登出"))
	out.WriteString(`</button></form>`)
	out.WriteString(`</div>`)
}

// writeCommentForm 渲染评论表单。
func writeCommentForm(out *strings.Builder, runtime *TemplateRuntime, ctx *RequestContext,
	data any, postID uint, csrfToken string, mailEnabled bool) {

	rememberedCommenter := dataValue(data, "RememberedCommenter")
	currentUser := dataValue(data, "CurrentUser")
	hasCurrentUser := !isNilAny(currentUser)

	out.WriteString(`<div id="comment-form-home"></div>`)
	out.WriteString(`<form class="comment-form" id="comment-form" method="post" action="/comment">`)
	out.WriteString(`<input type="hidden" name="post_id" value="`)
	out.WriteString(strconv.FormatUint(uint64(postID), 10))
	out.WriteString(`">`)
	out.WriteString(`<input type="hidden" name="parent_id" value="0">`)
	out.WriteString(`<input type="hidden" name="reply_to_id" value="0">`)
	if csrfToken != "" {
		out.WriteString(`<input type="hidden" name="_csrf" value="`)
		out.WriteString(html.EscapeString(csrfToken))
		out.WriteString(`">`)
	}

	if !hasCurrentUser {
		remAuthor, _ := dataValue(rememberedCommenter, "Author").(string)
		remEmail, _ := dataValue(rememberedCommenter, "Email").(string)
		remURL, _ := dataValue(rememberedCommenter, "URL").(string)

		out.WriteString(`<p><label class="sr-only" for="comment-author">`)
		out.WriteString(ctx.T("昵称"))
		out.WriteString(`</label><input id="comment-author" type="text" name="author" value="`)
		out.WriteString(html.EscapeString(remAuthor))
		out.WriteString(`" placeholder="`)
		out.WriteString(ctx.T("昵称 *"))
		out.WriteString(`" autocomplete="name" required></p>`)

		out.WriteString(`<p><label class="sr-only" for="comment-email">`)
		out.WriteString(ctx.T("邮箱"))
		out.WriteString(`</label><input id="comment-email" type="email" name="email" value="`)
		out.WriteString(html.EscapeString(remEmail))
		out.WriteString(`" placeholder="`)
		out.WriteString(ctx.T("邮箱 *(不公开)"))
		out.WriteString(`" autocomplete="email" required></p>`)

		out.WriteString(`<p><label class="sr-only" for="comment-url">`)
		out.WriteString(ctx.T("网站"))
		out.WriteString(`</label><input id="comment-url" type="url" name="url" value="`)
		out.WriteString(html.EscapeString(remURL))
		out.WriteString(`" placeholder="`)
		out.WriteString(ctx.T("网站(可选)"))
		out.WriteString(`" autocomplete="url"></p>`)
	}

	out.WriteString(`<p class="hp"><input type="text" name="website" tabindex="-1" autocomplete="off"></p>`)
	out.WriteString(`<p><label class="sr-only" for="comment-content">`)
	out.WriteString(ctx.T("评论内容"))
	out.WriteString(`</label><textarea id="comment-content" name="content" rows="5" placeholder="`)
	out.WriteString(ctx.T("说点什么吧…… *"))
	out.WriteString(`" required></textarea></p>`)

	// slot: comment.form.after_textarea
	if h := hooks(runtime); h != nil {
		var slotBuf strings.Builder
		h.DoAction(requestContext(ctx), hook.HookCommentFormAfterTextarea, &slotBuf,
			hook.CommentFormAfterTextareaData{Data: data})
		out.WriteString(slotBuf.String())
	}

	if mailEnabled {
		out.WriteString(`<p><label><input type="checkbox" name="notify"> `)
		out.WriteString(ctx.T("有回复时邮件通知我"))
		out.WriteString(`</label></p>`)
	}

	out.WriteString(`<p class="comment-form-actions"><button type="submit">`)
	out.WriteString(ctx.T("提交评论"))
	out.WriteString(`</button><button type="button" class="comment-cancel-reply" data-cancel-reply hidden>`)
	out.WriteString(ctx.T("取消回复"))
	out.WriteString(`</button></p>`)
	out.WriteString(`<p class="comment-tip">`)
	out.WriteString(ctx.T("评论提交后需经审核才会显示。"))
	out.WriteString(`</p>`)
	out.WriteString(`</form>`)
}

// ========== 辅助函数 ==========

// commentToMap 将评论(struct 或 map)转换为 map[string]any，供自定义模板使用。
func commentToMap(v any) map[string]any {
	result := make(map[string]any)
	if m, ok := v.(map[string]any); ok {
		for k, val := range m {
			result[k] = val
		}
		return result
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return result
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return result
	}
	t := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		field := t.Field(i)
		if field.IsExported() {
			result[field.Name] = rv.Field(i).Interface()
		}
	}
	return result
}

// reflectBoolField 通过反射获取 bool 字段值。
func reflectBoolField(v any, name string) bool {
	if m, ok := v.(map[string]any); ok {
		b, _ := m[name].(bool)
		return b
	}
	rv := indirectValue(v)
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return false
	}
	f := rv.FieldByName(name)
	if f.IsValid() && f.Kind() == reflect.Bool {
		return f.Bool()
	}
	return false
}

// reflectTimeField 通过反射获取 time.Time 字段值。
func reflectTimeField(v any, name string) time.Time {
	if m, ok := v.(map[string]any); ok {
		if t, ok := m[name].(time.Time); ok {
			return t
		}
		return time.Time{}
	}
	rv := indirectValue(v)
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return time.Time{}
	}
	f := rv.FieldByName(name)
	if f.IsValid() && f.Type() == reflect.TypeOf(time.Time{}) {
		return f.Interface().(time.Time)
	}
	return time.Time{}
}

// toInt 将 any 转换为 int（处理 int, int64, float64 等常见类型）。
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case uint:
		return int(n), true
	default:
		return 0, false
	}
}
