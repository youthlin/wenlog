package render

import (
	"fmt"
	"html"
	"html/template"
	"reflect"
	"strings"

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
	provider := runtime.hookInvokeProvider.Load()
	if provider == nil || provider.fn == nil {
		return nil
	}
	return provider.fn(ctx, name, args...)
}

// themeOption 在模板中使用 {{themeOption "<optionID>"}}
func themeOption(runtime *TemplateRuntime, ctx *RequestContext, optionID string) string {
	if runtime == nil {
		return ""
	}
	provider := runtime.optionProvider.Load()
	if provider == nil || provider.fn == nil {
		return ""
	}
	return provider.fn(ctx, optionID)
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
	if ctx == nil {
		return html.EscapeString(msg)
	}
	tr := dataValue(ctx.Data, "th")
	rv := reflect.ValueOf(tr)
	if !rv.IsValid() {
		return html.EscapeString(msg)
	}
	method := rv.MethodByName("T")
	if !method.IsValid() || method.Type().NumIn() != 1 || method.Type().In(0).Kind() != reflect.String || method.Type().NumOut() != 1 || method.Type().Out(0).Kind() != reflect.String {
		return html.EscapeString(msg)
	}
	out := method.Call([]reflect.Value{reflect.ValueOf(msg)})
	return html.EscapeString(out[0].String())
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
		key := "Menu" + strings.ToUpper(location[:1]) + location[1:]
		if items := anySlice(dataValue(data, key)); len(items) > 0 {
			return items
		}
	}
	return anySlice(dataValue(data, "Menu"))
}

func menuLocationItems(menus any, location string) []any {
	rv := reflect.ValueOf(menus)
	for rv.IsValid() && (rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer) {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil
	}
	v := rv.MapIndex(reflect.ValueOf(location))
	if !v.IsValid() || !v.CanInterface() {
		return nil
	}
	return anySlice(v.Interface())
}

func anySlice(v any) []any {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && (rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer) {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil
	}
	items := make([]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if item.CanInterface() {
			items = append(items, item.Interface())
		}
	}
	return items
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

func isDraft(ctx *RequestContext) bool {
	if ctx == nil {
		return false
	}
	value, _ := dataValue(ctx.Data, "IsDraft").(bool)
	return value
}
