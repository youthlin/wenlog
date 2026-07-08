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

	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/model"
	"github.com/youthlin/wenlog/internal/permalink"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/util"
	"github.com/youthlin/wenlog/internal/wxr"
)

// hookInvoke 在模板中使用 {{hook_invoke "FuncName" "argName" argValue ...}}
// 其中 FuncName 是在 functions.goyaegi 中通过 RegisterFunc 注册的导出函数（大写开头）。
func hookInvoke(ctx *RequestContext, name string, args ...any) any {
	if ctx == nil || ctx.Runtime == nil {
		return nil
	}
	provider := ctx.Runtime.current().HookInvoke
	if provider == nil {
		return nil
	}
	return provider(ctx, name, args...)
}

// themeOption 在模板中使用 {{theme_option "<optionID>"}}
func themeOption(ctx *RequestContext, optionID string) string {
	if ctx == nil || ctx.Runtime == nil {
		return ""
	}
	provider := ctx.Runtime.current().ThemeOption
	if provider == nil {
		return ""
	}
	return provider(ctx, optionID)
}

// widgetOption 模板函数：读取当前渲染组件的选项值。
// 模板中使用 {{widget_option "<key>"}}。
func widgetOption(ctx *RequestContext, key string) string {
	if ctx == nil || ctx.WidgetOptions == nil {
		return ""
	}
	return ctx.WidgetOptions[key]
}

func postTitle(ctx *RequestContext, post any) template.HTML {
	title := postTitleText(ctx, post)
	title = html.EscapeString(title)
	if title == "" {
		return ""
	}
	if isDraft(ctx) {
		title += ` <span class="draft-badge">` + translate(ctx, "草稿预览") + `</span>`
	}
	return template.HTML(`<h1 class="post-title">` + title + `</h1>`)
}

func postTitleText(ctx *RequestContext, post any) string {
	title := reflectStringField(post, "Title")
	if h := hooks(ctx.Runtime); h != nil {
		reqCtx := requestContext(ctx)
		v := h.ApplyFilters(reqCtx, hook.FilterPostTitle, title, postViewPayload(ctx, post))
		title = textFromFilterValue(v, title)
	}
	return title
}

func translate(ctx *RequestContext, msg string) string {
	if ctx != nil {
		if tr, ok := dataValue(ctx.Data, "th").(interface{ T(string, ...any) string }); ok && tr != nil {
			return html.EscapeString(tr.T(msg))
		}
	}
	return html.EscapeString(msg)
}

func textFromFilterValue(v any, fallback string) string {
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

func postExcerpt(ctx *RequestContext, post any) template.HTML {
	html := postExcerptHTMLAny(post)
	if h := hooks(ctx.Runtime); h != nil {
		reqCtx := requestContext(ctx)
		v := h.ApplyFilters(reqCtx, hook.FilterPostExcerptHTML, string(html), postViewPayload(ctx, post))
		html = trustedHTMLFromFilterValue(v)
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

// postContent 输出文章正文，并应用 post.content_html 和 post.footer_html filter。
func postContent(r *RequestContext, post any) template.HTML {
	html := postContentHTML(post)
	if h := hooks(r.Runtime); h != nil {
		ctx := requestContext(r)
		payload := postViewPayload(r, post)
		v := h.ApplyFilters(ctx, hook.FilterPostContentHTML, string(html), payload)
		html = trustedHTMLFromFilterValue(v)
		v = h.ApplyFilters(ctx, hook.FilterPostFooterHTML, string(html), payload)
		html = trustedHTMLFromFilterValue(v)
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

// commentContent 输出评论正文，并应用 comment.content_html filter。
func commentContent(ctx *RequestContext, comment any) template.HTML {
	html := template.HTML(html.EscapeString(reflectStringField(comment, "Content")))
	if h := hooks(ctx.Runtime); h != nil {
		reqCtx := requestContext(ctx)
		v := h.ApplyFilters(reqCtx, hook.FilterCommentContentHTML, string(html), commentViewPayload(comment))
		html = trustedHTMLFromFilterValue(v)
	}
	return html
}

func postViewPayload(ctx *RequestContext, post any) any {
	if view := hook.PostViewOf(post, dataLoader(ctx)); view != nil {
		return *view
	}
	return post
}

func dataLoader(ctx *RequestContext) *store.DataLoader {
	if ctx == nil || ctx.ThemeLoader == nil {
		return nil
	}
	loader, _ := ctx.ThemeLoader.(*store.DataLoader)
	return loader
}

func commentViewPayload(comment any) any {
	if view := hook.CommentViewOf(comment); view != nil {
		return *view
	}
	return comment
}

// trustedHTMLFromFilterValue converts a standard HTML filter result into template.HTML.
//
// Only use this for filters whose contract explicitly returns trusted/sanitized HTML,
// such as post.content_html, comment.content_html, widget.render_html, and head.meta.
func trustedHTMLFromFilterValue(v any) template.HTML {
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
	util.WriteString(&out, `<div class="post-tags">`)
	for _, tag := range tags {
		name := html.EscapeString(reflectStringField(tag, "Name"))
		slug := reflectStringField(tag, "Slug")
		if name == "" || slug == "" {
			continue
		}
		util.WriteString(&out, "\n  ")
		util.WriteString(&out, `<a class="tag" href="`)
		util.WriteString(&out, html.EscapeString(permalink.Tag(slug)))
		util.WriteString(&out, `">#`)
		util.WriteString(&out, name)
		util.WriteString(&out, `</a>`)
	}
	util.WriteString(&out, "\n")
	util.WriteString(&out, `</div>`)
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
	util.WriteString(&out, `<nav class="`)
	util.WriteString(&out, html.EscapeString(class))
	util.WriteString(&out, `">`)
	util.WriteString(&out, `<span class="prev">`)
	if !isNilAny(prev) {
		util.WriteString(&out, `<a href="`)
		util.WriteString(&out, html.EscapeString(postURL(prev)))
		util.WriteString(&out, `">← `)
		util.WriteString(&out, html.EscapeString(reflectStringField(prev, "Title")))
		util.WriteString(&out, `</a>`)
	}
	util.WriteString(&out, `</span><span class="next">`)
	if !isNilAny(next) {
		util.WriteString(&out, `<a href="`)
		util.WriteString(&out, html.EscapeString(postURL(next)))
		util.WriteString(&out, `">`)
		util.WriteString(&out, html.EscapeString(reflectStringField(next, "Title")))
		util.WriteString(&out, ` →</a>`)
	}
	util.WriteString(&out, `</span></nav>`)
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
	util.WriteString(&out, `<nav class="`)
	util.WriteString(&out, html.EscapeString(class))
	util.WriteString(&out, `">`)
	util.WriteString(&out, `<ul class="menu-items">`)
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
		util.WriteString(&out, `<li class="menu-item"><a href="`)
		util.WriteString(&out, html.EscapeString(url))
		util.WriteString(&out, `"`)
		if target != "" {
			util.WriteString(&out, ` target="`)
			util.WriteString(&out, html.EscapeString(target))
			util.WriteString(&out, `" rel="noreferrer"`)
		}
		util.WriteString(&out, `>`)
		util.WriteString(&out, title)
		util.WriteString(&out, `</a>`)
		children := reflectSliceField(item, "Children")
		if len(children) > 0 {
			util.WriteString(&out, `<ul class="sub-menu">`)
			for _, child := range children {
				writeMenuItem(&out, child)
			}
			util.WriteString(&out, `</ul>`)
		}
		util.WriteString(&out, `</li>`)
	}
	util.WriteString(&out, `</ul></nav>`)
	return template.HTML(out.String())
}

func writeMenuItem(out *strings.Builder, item any) {
	title := html.EscapeString(reflectStringField(item, "Title"))
	url := reflectStringField(item, "URL")
	if title == "" || url == "" {
		return
	}
	target := reflectStringField(item, "Target")
	util.WriteString(out, `<li class="menu-item"><a href="`)
	util.WriteString(out, html.EscapeString(url))
	util.WriteString(out, `"`)
	if target != "" {
		util.WriteString(out, ` target="`)
		util.WriteString(out, html.EscapeString(target))
		util.WriteString(out, `" rel="noreferrer"`)
	}
	util.WriteString(out, `>`)
	util.WriteString(out, title)
	util.WriteString(out, `</a>`)
	children := reflectSliceField(item, "Children")
	if len(children) > 0 {
		util.WriteString(out, `<ul class="sub-menu">`)
		for _, child := range children {
			writeMenuItem(out, child)
		}
		util.WriteString(out, `</ul>`)
	}
	util.WriteString(out, `</li>`)
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
		classes = append(classes, "page-list")
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
		classes = append(classes, "is-reply")
	} else {
		classes = append(classes, "is-root")
	}
	if status := reflectStringField(comment, "Status"); status != "" {
		classes = append(classes, "status-"+slugClass(status))
	}
	return strings.Join(uniqueClasses(classes), " ")
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

// headMeta 生成 OpenGraph / Twitter Card meta 标签。
// 在模板 <head> 中使用 {{headMeta .}} 调用。
// 生成的 HTML 会经过 head.meta filter，插件可改写、追加或清空 meta 标签。
func headMeta(ctx *RequestContext, data any) template.HTML {
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
	if h := hooks(ctx.Runtime); h != nil {
		v := h.ApplyFilters(requestContext(ctx), hook.FilterHeadMeta, string(html), hook.HeadMetaView{
			Title:        title,
			Description:  desc,
			CanonicalURL: url,
			SiteName:     siteName,
			SiteLogo:     siteLogo,
			OGType:       ogType,
			TwitterCard:  card,
		})
		html = trustedHTMLFromFilterValue(v)
	}
	return html
}

func writeMeta(b *strings.Builder, property, content string) {
	util.WriteString(b, `<meta property="`)
	util.WriteString(b, html.EscapeString(property))
	util.WriteString(b, `" content="`)
	util.WriteString(b, html.EscapeString(content))
	util.WriteString(b, `">`)
	b.WriteByte('\n')
}

func writeMetaName(b *strings.Builder, name, content string) {
	util.WriteString(b, `<meta name="`)
	util.WriteString(b, html.EscapeString(name))
	util.WriteString(b, `" content="`)
	util.WriteString(b, html.EscapeString(content))
	util.WriteString(b, `">`)
	util.WriteString(b, "\n")
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

// indent 返回换行 + depth*2 个空格的缩进字符串。
func indent(depth int) string {
	if depth <= 0 {
		return "\n"
	}
	return "\n" + strings.Repeat("  ", depth)
}

// listComments 渲染评论列表和分页。
// 模板调用: {{listComments .}} 或 {{listComments . "ol" 44 "my_comment"}}
// 参数: args[0]=data(必填), args[1]=style(默认"ul"), args[2]=avatarSize(默认40), args[3]=callback模板名(默认"comment_item")
func listComments(ctx *RequestContext, args ...any) template.HTML {
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
	util.WriteString(&out, "<")
	util.WriteString(&out, listTag)
	util.WriteString(&out, ` class="comment-list">`)

	for _, c := range comments {
		parentID := reflectUintField(c, "ParentID")
		if parentID != 0 {
			continue
		}
		util.WriteString(&out, indent(1))
		writeCommentItem(&out, ctx, data, c, comments, callbackTpl, hasCustomTpl, avatarSize, defaultAvatar, listTag, 1)
	}

	util.WriteString(&out, indent(0))
	util.WriteString(&out, "</")
	util.WriteString(&out, listTag)
	util.WriteString(&out, ">")

	return template.HTML(out.String())
}

// commentForm 渲染登录提示和评论表单（或"评论已关闭"提示）。
// 模板调用: {{commentForm .}}
func commentForm(ctx *RequestContext, data any) template.HTML {
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

	util.WriteString(&out, `<div id="comment-form-home"></div>`)
	util.WriteString(&out, `<div id="comment-form-box" class="comment-form-box">`)

	// 登录提示和评论表单作为一个整体移动，避免回复时两者布局不一致。
	writeCommentLoginTip(&out, ctx, data, csrfToken)

	// 评论表单或关闭提示
	if commentOpen {
		writeCommentForm(&out, ctx, data, postID, csrfToken, mailEnabled)
	} else {
		util.WriteString(&out, `<p class="comment-closed">`)
		util.WriteString(&out, ctx.T("评论已关闭"))
		util.WriteString(&out, `</p>`)
	}
	util.WriteString(&out, `</div>`)

	return template.HTML(out.String())
}

// writeCommentItem 渲染单条评论（含 <li> 包裹）及其子回复。
func writeCommentItem(out *strings.Builder, ctx *RequestContext,
	data any, comment any, allComments []any, callbackTpl string, hasCustomTpl bool,
	avatarSize int, defaultAvatar string, listTag string, depth int) {

	id := reflectUintField(comment, "ID")
	status := reflectStringField(comment, "Status")

	classes := commentClass(comment)
	if status == "pending" {
		classes += " comment-pending"
	}

	util.WriteString(out, `<li class="`)
	util.WriteString(out, html.EscapeString(classes))
	util.WriteString(out, `" id="comment-`)
	util.WriteString(out, strconv.FormatUint(uint64(id), 10))
	util.WriteString(out, `">`)

	if hasCustomTpl {
		itemData := commentToMap(comment)
		itemData["DefaultAvatar"] = defaultAvatar
		if th := dataValue(data, "th"); th != nil {
			itemData["th"] = th
		}
		var buf strings.Builder
		if err := ctx.Template.ExecuteTemplate(&buf, callbackTpl, itemData); err == nil {
			util.WriteString(out, buf.String())
		}
	} else {
		writeDefaultCommentItem(out, ctx, data, comment, avatarSize, defaultAvatar, depth)
	}

	// 子回复
	writeCommentChildren(out, ctx, data, comment, allComments, callbackTpl, hasCustomTpl, avatarSize, defaultAvatar, listTag, depth)

	util.WriteString(out, indent(depth))
	util.WriteString(out, `</li>`)
}

// writeDefaultCommentItem 生成默认评论 HTML（与 default 主题结构一致）。
func writeDefaultCommentItem(out *strings.Builder, ctx *RequestContext,
	data any, comment any, avatarSize int, defaultAvatar string, depth int) {

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
	util.WriteString(out, indent(depth+1))
	util.WriteString(out, `<div class="comment-head">`)
	// avatar
	util.WriteString(out, `<img class="avatar" src="`)
	util.WriteString(out, html.EscapeString(avatarURL(email, defaultAvatar)))
	util.WriteString(out, `" alt="" width="`)
	util.WriteString(out, sizeStr)
	util.WriteString(out, `" height="`)
	util.WriteString(out, sizeStr)
	util.WriteString(out, `">`)
	// comment-id
	util.WriteString(out, `<a class="comment-id" href="#comment-`)
	util.WriteString(out, strconv.FormatUint(uint64(id), 10))
	util.WriteString(out, `">#`)
	util.WriteString(out, strconv.FormatUint(uint64(id), 10))
	util.WriteString(out, `</a>`)
	// author
	util.WriteString(out, `<span class="comment-author">`)
	util.WriteString(out, author)
	util.WriteString(out, `</span>`)
	// badges
	if commenterRole != "" {
		var roleTitle string
		switch commenterRole {
		case "author":
			roleTitle = ctx.T("文章作者")
		case "admin":
			roleTitle = ctx.T("管理员")
		default:
			roleTitle = ctx.T("登录用户")
		}
		util.WriteString(out, `<span class="comment-badge comment-badge-`)
		util.WriteString(out, html.EscapeString(commenterRole))
		util.WriteString(out, `" title="`)
		util.WriteString(out, roleTitle)
		util.WriteString(out, `"></span>`)
	}
	if notifyOnReply {
		util.WriteString(out, `<span class="comment-badge comment-badge-notify" title="`)
		util.WriteString(out, ctx.T("回复时邮件通知"))
		util.WriteString(out, `"></span>`)
	}
	// time
	util.WriteString(out, `<time>`)
	if !createdAt.IsZero() {
		util.WriteString(out, html.EscapeString(createdAt.Format("2006-01-02 15:04")))
	}
	util.WriteString(out, `</time>`)
	// pending badge
	if status == "pending" {
		util.WriteString(out, `<span class="comment-pending-badge">`)
		util.WriteString(out, ctx.T("评论审核中,当前仅自己可见"))
		util.WriteString(out, `</span>`)
	}
	util.WriteString(out, indent(depth+1))
	util.WriteString(out, `</div>`)

	// comment-body
	util.WriteString(out, indent(depth+1))
	util.WriteString(out, `<div class="comment-body">`)
	if parentID != 0 && replyToAuthor != "" && replyToID > 0 {
		util.WriteString(out, `<a class="comment-reply-to" href="#comment-`)
		util.WriteString(out, strconv.FormatUint(uint64(replyToID), 10))
		util.WriteString(out, `">`)
		util.WriteString(out, html.EscapeString(ctx.T("回复 @%s", replyToAuthor)))
		util.WriteString(out, `</a>`)
	}
	util.WriteString(out, string(commentContent(ctx, comment)))
	util.WriteString(out, indent(depth+1))
	util.WriteString(out, `</div>`)

	// reply button
	util.WriteString(out, indent(depth+1))
	util.WriteString(out, `<button class="comment-reply" type="button" data-reply="`)
	util.WriteString(out, strconv.FormatUint(uint64(id), 10))
	util.WriteString(out, `" data-reply-target="comment-`)
	util.WriteString(out, strconv.FormatUint(uint64(id), 10))
	util.WriteString(out, `">`)
	util.WriteString(out, ctx.T("回复"))
	util.WriteString(out, `</button>`)
}

// writeCommentChildren 渲染评论的子回复列表。
func writeCommentChildren(out *strings.Builder, ctx *RequestContext,
	data any, parent any, allComments []any, callbackTpl string, hasCustomTpl bool,
	avatarSize int, defaultAvatar string, listTag string, depth int) {

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

	util.WriteString(out, indent(depth+1))
	util.WriteString(out, "<")
	util.WriteString(out, childrenTag)
	util.WriteString(out, ` class="comment-children">`)

	for _, child := range children {
		util.WriteString(out, indent(depth+2))
		writeCommentItem(out, ctx, data, child, allComments, callbackTpl, hasCustomTpl, avatarSize, defaultAvatar, listTag, depth+2)
	}

	util.WriteString(out, indent(depth+1))
	util.WriteString(out, "</")
	util.WriteString(out, childrenTag)
	util.WriteString(out, ">")
}

// commentsPagination 渲染评论分页导航。
// 模板调用: {{comments_pagination .}} 或 {{comments_pagination . 2}}
func commentsPagination(ctx *RequestContext, data any, midSize ...int) template.HTML {
	var out strings.Builder
	writeCommentPagination(&out, ctx, data, midSize...)
	return template.HTML(out.String())
}

// postsPagination 渲染文章列表分页导航，类似 WordPress paginate_links。
// midSize 控制当前页左右展示的页码数量，默认 2。
func postsPagination(ctx *RequestContext, data any, midSize ...int) template.HTML {
	pager, ok := pagerData(dataValue(data, "Pager"))
	if !ok {
		return ""
	}
	pages, _ := toInt(pager["Pages"])
	if pages <= 1 {
		return ""
	}
	page, _ := toInt(pager["Page"])
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	baseURL, _ := pager["BaseURL"].(string)
	sep, _ := pager["Sep"].(string)
	if sep == "" {
		sep = "?"
	}
	ms := 2
	if len(midSize) > 0 && midSize[0] >= 0 {
		ms = midSize[0]
	}

	var out strings.Builder
	util.WriteString(&out, `<nav class="pagination posts-pagination"><div class="nav-links">`)
	if page > 1 {
		writePostPageLink(&out, ctx.T("上一页"), postPageURL(baseURL, sep, page-1), "prev")
	}
	writePageNumbers(&out, page, pages, ms, func(i int) {
		writePostPageLink(&out, strconv.Itoa(i), postPageURL(baseURL, sep, i), "")
	})
	if page < pages {
		writePostPageLink(&out, ctx.T("下一页"), postPageURL(baseURL, sep, page+1), "next")
	}
	util.WriteString(&out, `</div></nav>`)
	return template.HTML(out.String())
}

func pagerData(pagerAny any) (map[string]any, bool) {
	if pagerAny == nil {
		return nil, false
	}
	if pager, ok := pagerAny.(map[string]any); ok {
		return pager, true
	}
	if pager, ok := pagerAny.(gin.H); ok {
		return map[string]any(pager), true
	}
	return nil, false
}

func postPageURL(baseURL, sep string, page int) string {
	if page <= 1 {
		return baseURL
	}
	return baseURL + sep + "page=" + strconv.Itoa(page)
}

func writePostPageLink(out *strings.Builder, label, href, extraClass string) {
	util.WriteString(out, `<a class="page-numbers`)
	if extraClass != "" {
		util.WriteString(out, ` `)
		util.WriteString(out, html.EscapeString(extraClass))
	}
	util.WriteString(out, `" href="`)
	util.WriteString(out, html.EscapeString(href))
	util.WriteString(out, `">`)
	util.WriteString(out, html.EscapeString(label))
	util.WriteString(out, `</a>`)
}

func writePageNumbers(out *strings.Builder, page, pages, midSize int, writeLink func(page int)) {
	wroteDots := false
	for i := 1; i <= pages; i++ {
		visible := i == 1 || i == pages || (i >= page-midSize && i <= page+midSize)
		if !visible {
			if !wroteDots {
				util.WriteString(out, `<span class="page-numbers dots">&hellip;</span>`)
				wroteDots = true
			}
			continue
		}
		wroteDots = false
		if i == page {
			util.WriteString(out, `<span class="page-numbers current">`)
			util.WriteString(out, strconv.Itoa(i))
			util.WriteString(out, `</span>`)
			continue
		}
		writeLink(i)
	}
}

// writeCommentPagination 渲染评论分页导航。
func writeCommentPagination(out *strings.Builder, ctx *RequestContext, data any, midSize ...int) {
	pager, ok := pagerData(dataValue(data, "CommentPager"))
	if !ok {
		return
	}
	pages, _ := toInt(pager["Pages"])
	if pages <= 1 {
		return
	}
	page, _ := toInt(pager["Page"])
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	baseURL, _ := pager["BaseURL"].(string)
	ms := 2
	if len(midSize) > 0 && midSize[0] >= 0 {
		ms = midSize[0]
	}

	util.WriteString(out, `<nav class="pagination comment-pagination"><div class="nav-links">`)
	if page > 1 {
		writeCommentPageLink(out, ctx.T("上一页"), baseURL, page-1, "prev")
	}
	writePageNumbers(out, page, pages, ms, func(i int) {
		writeCommentPageLink(out, strconv.Itoa(i), baseURL, i, "")
	})
	if page < pages {
		writeCommentPageLink(out, ctx.T("下一页"), baseURL, page+1, "next")
	}
	util.WriteString(out, `</div></nav>`)
}

func writeCommentPageLink(out *strings.Builder, label, baseURL string, page int, extraClass string) {
	util.WriteString(out, `<a class="page-numbers`)
	if extraClass != "" {
		util.WriteString(out, ` `)
		util.WriteString(out, html.EscapeString(extraClass))
	}
	util.WriteString(out, `" href="`)
	util.WriteString(out, html.EscapeString(baseURL))
	util.WriteString(out, `?cpage=`)
	util.WriteString(out, strconv.Itoa(page))
	util.WriteString(out, `#comments" data-cpage="`)
	util.WriteString(out, strconv.Itoa(page))
	util.WriteString(out, `">`)
	util.WriteString(out, html.EscapeString(label))
	util.WriteString(out, `</a>`)
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

	util.WriteString(out, `<div class="comment-login-tip">`)
	// 使用 th.T 格式化（含 HTML 标签）
	if th := dataValue(data, "th"); th != nil {
		if tr, ok := th.(interface{ T(string, ...any) string }); ok {
			util.WriteString(out, tr.T("以 <strong>%s</strong> 的身份发表评论，", html.EscapeString(name)))
		}
	}
	util.WriteString(out, `<form class="inline" method="post" action="/auth/logout">`)
	if csrfToken != "" {
		util.WriteString(out, `<input type="hidden" name="_csrf" value="`)
		util.WriteString(out, html.EscapeString(csrfToken))
		util.WriteString(out, `">`)
	}
	util.WriteString(out, `<button type="submit">`)
	util.WriteString(out, ctx.T("登出"))
	util.WriteString(out, `</button></form>`)
	util.WriteString(out, `</div>`)
}

// writeCommentForm 渲染评论表单。
func writeCommentForm(out *strings.Builder, ctx *RequestContext,
	data any, postID uint, csrfToken string, mailEnabled bool) {

	rememberedCommenter := dataValue(data, "RememberedCommenter")
	currentUser := dataValue(data, "CurrentUser")
	hasCurrentUser := !isNilAny(currentUser)

	util.WriteString(out, `<form class="comment-form" id="comment-form" method="post" action="/comment">`)
	util.WriteString(out, `<input type="hidden" name="post_id" value="`)
	util.WriteString(out, strconv.FormatUint(uint64(postID), 10))
	util.WriteString(out, `">`)
	util.WriteString(out, `<input type="hidden" name="parent_id" value="0">`)
	util.WriteString(out, `<input type="hidden" name="reply_to_id" value="0">`)
	if csrfToken != "" {
		util.WriteString(out, `<input type="hidden" name="_csrf" value="`)
		util.WriteString(out, html.EscapeString(csrfToken))
		util.WriteString(out, `">`)
	}

	if !hasCurrentUser {
		remAuthor, _ := dataValue(rememberedCommenter, "Author").(string)
		remEmail, _ := dataValue(rememberedCommenter, "Email").(string)
		remURL, _ := dataValue(rememberedCommenter, "URL").(string)

		util.WriteString(out, `<p><label class="sr-only" for="comment-author">`)
		util.WriteString(out, ctx.T("昵称"))
		util.WriteString(out, `</label><input id="comment-author" type="text" name="author" value="`)
		util.WriteString(out, html.EscapeString(remAuthor))
		util.WriteString(out, `" placeholder="`)
		util.WriteString(out, ctx.T("昵称 *"))
		util.WriteString(out, `" autocomplete="name" required></p>`)

		util.WriteString(out, `<p><label class="sr-only" for="comment-email">`)
		util.WriteString(out, ctx.T("邮箱"))
		util.WriteString(out, `</label><input id="comment-email" type="email" name="email" value="`)
		util.WriteString(out, html.EscapeString(remEmail))
		util.WriteString(out, `" placeholder="`)
		util.WriteString(out, ctx.T("邮箱 *(不公开)"))
		util.WriteString(out, `" autocomplete="email" required></p>`)

		util.WriteString(out, `<p><label class="sr-only" for="comment-url">`)
		util.WriteString(out, ctx.T("网站"))
		util.WriteString(out, `</label><input id="comment-url" type="url" name="url" value="`)
		util.WriteString(out, html.EscapeString(remURL))
		util.WriteString(out, `" placeholder="`)
		util.WriteString(out, ctx.T("网站(可选)"))
		util.WriteString(out, `" autocomplete="url"></p>`)
	}

	util.WriteString(out, `<p class="hp"><input type="text" name="website" tabindex="-1" autocomplete="off"></p>`)
	util.WriteString(out, `<p><label class="sr-only" for="comment-content">`)
	util.WriteString(out, ctx.T("评论内容"))
	util.WriteString(out, `</label><textarea id="comment-content" name="content" rows="5" placeholder="`)
	util.WriteString(out, ctx.T("说点什么吧…… *"))
	util.WriteString(out, `" required></textarea></p>`)

	// slot: comment.form.after_textarea
	if h := hooks(ctx.Runtime); h != nil {
		var slotBuf strings.Builder
		h.DoAction(
			requestContext(ctx),
			hook.ActionCommentFormAfterTextarea,
			&slotBuf,
			data,
		)
		util.WriteString(out, slotBuf.String())
	}

	if mailEnabled {
		util.WriteString(out, `<p><label><input type="checkbox" name="notify"> `)
		util.WriteString(out, ctx.T("有回复时邮件通知我"))
		util.WriteString(out, `</label></p>`)
	}

	util.WriteString(out, `<p class="comment-form-actions"><button type="submit">`)
	util.WriteString(out, ctx.T("提交评论"))
	util.WriteString(out, `</button><button type="button" class="comment-cancel-reply" data-cancel-reply hidden>`)
	util.WriteString(out, ctx.T("取消回复"))
	util.WriteString(out, `</button></p>`)
	util.WriteString(out, `<p class="comment-tip">`)
	util.WriteString(out, ctx.T("评论提交后需经审核才会显示。"))
	util.WriteString(out, `</p>`)
	util.WriteString(out, `</form>`)
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
