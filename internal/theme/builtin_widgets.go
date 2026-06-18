package theme

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"html/template"
	"strconv"
	"strings"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"
)

// builtinWidgets 注册所有内置 Widget。
func init() {
	Register(&userInfoWidget{})
	Register(&searchWidget{})
	Register(&sayingWidget{})
	Register(&recentPostsWidget{})
	Register(&recentCommentsWidget{})
	Register(&archiveMonthsWidget{})
	Register(&categoriesWidget{})
	Register(&tagsWidget{})
}

// --- 辅助函数 ---

func widgetAvatarURL(email, defaultAvatar string) string {
	hash := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email))))
	return "https://cn.cravatar.com/avatar/" + hex.EncodeToString(hash[:]) +
		"?s=" + strconv.Itoa(consts.AvatarSizeSmall) +
		"&d=" + util.NormalizeDefaultAvatar(defaultAvatar)
}

func widgetPostURL(p model.Post) string {
	return permalink.Post(&p)
}

func widgetCategoryURL(slug string) string {
	return permalink.Category(slug)
}

func widgetTagURL(slug string) string {
	return permalink.Tag(slug)
}

// --- user_info ---

type userInfoWidget struct{}

func (w *userInfoWidget) Name() string { return "user_info" }

func (w *userInfoWidget) Render(ctx context.Context, st *store.Store, settings WidgetSettings) (template.HTML, error) {
	var buf bytes.Buffer
	buf.WriteString(`<section class="widget widget-user">`)
	if settings.CurrentUserID != 0 {
		buf.WriteString(`<h3>` + esc(settings.CurrentUserName) + `, 你好</h3>`)
		buf.WriteString(`<div class="widget-user-actions">`)
		buf.WriteString(`<a href="/admin">控制台</a>`)
		buf.WriteString(`<form method="post" action="/admin/logout">`)
		if settings.CSRFToken != "" {
			buf.WriteString(`<input type="hidden" name="_csrf" value="` + esc(settings.CSRFToken) + `">`)
		}
		buf.WriteString(`<button type="submit">退出</button>`)
		buf.WriteString(`</form></div>`)
	} else {
		buf.WriteString(`<h3>欢迎</h3>`)
		buf.WriteString(`<div class="widget-user-actions">`)
		buf.WriteString(`<a href="/admin/login">登录</a>`)
		if settings.RegistrationOpen {
			buf.WriteString(`<a href="/admin/register">注册</a>`)
		}
		buf.WriteString(`</div>`)
	}
	buf.WriteString(`</section>`)
	return template.HTML(buf.String()), nil
}

// --- search ---

type searchWidget struct{}

func (w *searchWidget) Name() string { return "search" }

func (w *searchWidget) Render(ctx context.Context, st *store.Store, settings WidgetSettings) (template.HTML, error) {
	return template.HTML(`<section class="widget widget-search">
<h3>` + esc("搜索") + `</h3>
<form method="get" action="/search">
<input type="search" name="q" placeholder="` + esc("输入关键词…") + `" value="` + esc(settings.Keyword) + `">
<button type="submit">` + esc("搜索") + `</button>
</form>
</section>`), nil
}

// --- saying (博主动态) ---

type sayingWidget struct{}

func (w *sayingWidget) Name() string { return "saying" }

func (w *sayingWidget) Render(ctx context.Context, st *store.Store, settings WidgetSettings) (template.HTML, error) {
	if settings.SayingPostID == 0 {
		return "", nil
	}
	items := st.SayingCommentItems(ctx, settings.SayingPostID, 5, 20)
	if len(items) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	buf.WriteString(`<section class="widget widget-saying"><h3>博主动态</h3>`)
	if len(items) > 0 {
		p := items[0].Post
		buf.WriteString(`<div class="widget-saying-author">`)
		buf.WriteString(`<img class="avatar" src="`)
		buf.WriteString(template.HTMLEscapeString(widgetAvatarURL(p.Author.Email, settings.DefaultAvatar)))
		buf.WriteString(`" alt="" width="32" height="32">`)
		name := p.Author.DisplayName
		if name == "" {
			name = p.Author.Username
		}
		buf.WriteString(`<span>` + template.HTMLEscapeString(name) + `</span>`)
		buf.WriteString(`</div>`)
	}
	buf.WriteString(`<ul>`)
	for _, item := range items {
		buf.WriteString(`<li><div class="widget-comment-line"><a href="`)
		buf.WriteString(template.HTMLEscapeString(item.CommentURL))
		buf.WriteString(`">`)
		buf.WriteString(template.HTMLEscapeString(item.Snippet))
		buf.WriteString(`</a></div></li>`)
	}
	buf.WriteString(`</ul></section>`)
	return template.HTML(buf.String()), nil
}

// --- recent_posts ---

type recentPostsWidget struct{}

func (w *recentPostsWidget) Name() string { return "recent_posts" }

func (w *recentPostsWidget) Render(ctx context.Context, st *store.Store, settings WidgetSettings) (template.HTML, error) {
	posts := st.RecentPosts(ctx, 8)
	if len(posts) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	buf.WriteString(`<section class="widget widget-recent-posts"><h3>近期文章</h3><ul>`)
	for _, p := range posts {
		buf.WriteString(`<li><a href="`)
		buf.WriteString(template.HTMLEscapeString(widgetPostURL(p)))
		buf.WriteString(`">`)
		buf.WriteString(template.HTMLEscapeString(p.Title))
		buf.WriteString(`</a></li>`)
	}
	buf.WriteString(`</ul></section>`)
	return template.HTML(buf.String()), nil
}

// --- recent_comments ---

type recentCommentsWidget struct{}

func (w *recentCommentsWidget) Name() string { return "recent_comments" }

func (w *recentCommentsWidget) Render(ctx context.Context, st *store.Store, settings WidgetSettings) (template.HTML, error) {
	items := st.RecentCommentItems(ctx, 8, 20)
	if len(items) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	buf.WriteString(`<section class="widget widget-recent-comments"><h3>近期评论</h3><ul>`)
	for _, item := range items {
		buf.WriteString(`<li><div class="widget-comment-line"><img class="avatar" src="`)
		buf.WriteString(template.HTMLEscapeString(widgetAvatarURL(item.Comment.Email, settings.DefaultAvatar)))
		buf.WriteString(`" alt="" width="24" height="24"><a href="`)
		buf.WriteString(template.HTMLEscapeString(item.CommentURL))
		buf.WriteString(`">`)
		buf.WriteString(template.HTMLEscapeString(item.Snippet))
		buf.WriteString(`</a></div><div class="widget-comment-meta">`)
		if item.AuthorURL != "" {
			buf.WriteString(`<a href="` + template.HTMLEscapeString(item.AuthorURL) + `" target="_blank" rel="nofollow noopener">`)
			buf.WriteString(template.HTMLEscapeString(item.Comment.Author))
			buf.WriteString(`</a>`)
		} else {
			buf.WriteString(template.HTMLEscapeString(item.Comment.Author))
		}
		buf.WriteString(` 发表在 <a href="`)
		buf.WriteString(template.HTMLEscapeString(item.CommentURL))
		buf.WriteString(`">`)
		buf.WriteString(template.HTMLEscapeString(item.Post.Title))
		buf.WriteString(`</a></div></li>`)
	}
	buf.WriteString(`</ul></section>`)
	return template.HTML(buf.String()), nil
}

// --- archive_months ---

type archiveMonthsWidget struct{}

func (w *archiveMonthsWidget) Name() string { return "archive_months" }

func (w *archiveMonthsWidget) Render(ctx context.Context, st *store.Store, settings WidgetSettings) (template.HTML, error) {
	months := st.ArchiveMonths(ctx)
	if len(months) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	buf.WriteString(`<section class="widget widget-archive"><h3>归档</h3>`)
	buf.WriteString(`<select onchange="if(this.value)location=this.value" data-searchable-select data-searchable-full-width data-search-placeholder="输入月份筛选…" data-search-empty="没有匹配的月份">`)
	buf.WriteString(`<option value="">选择月份…</option>`)
	for _, m := range months {
		buf.WriteString(`<option value="/archive#` + strconv.Itoa(m.Year) + `">`)
		buf.WriteString(strconv.Itoa(m.Year) + ` 年 ` + strconv.Itoa(m.Month) + ` 月(` + strconv.FormatInt(m.Count, 10) + `)</option>`)
	}
	buf.WriteString(`</select></section>`)
	return template.HTML(buf.String()), nil
}

// --- categories ---

type categoriesWidget struct{}

func (w *categoriesWidget) Name() string { return "categories" }

func (w *categoriesWidget) Render(ctx context.Context, st *store.Store, settings WidgetSettings) (template.HTML, error) {
	cats := st.AllCategories(ctx)
	if len(cats) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	buf.WriteString(`<section class="widget widget-categories"><h3>分类目录</h3>`)
	buf.WriteString(`<select onchange="if(this.value)location=this.value" data-searchable-select data-searchable-full-width data-search-placeholder="输入分类名筛选…" data-search-empty="没有匹配的分类">`)
	buf.WriteString(`<option value="">选择分类…</option>`)
	for _, c := range cats {
		buf.WriteString(`<option value="`)
		buf.WriteString(template.HTMLEscapeString(widgetCategoryURL(c.Slug)))
		buf.WriteString(`">`)
		buf.WriteString(template.HTMLEscapeString(c.Name))
		buf.WriteString(`</option>`)
	}
	buf.WriteString(`</select></section>`)
	return template.HTML(buf.String()), nil
}

// --- tags ---

type tagsWidget struct{}

func (w *tagsWidget) Name() string { return "tags" }

func (w *tagsWidget) Render(ctx context.Context, st *store.Store, settings WidgetSettings) (template.HTML, error) {
	tags := st.AllTags(ctx)
	if len(tags) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	buf.WriteString(`<section class="widget widget-tags"><h3>标签</h3>`)
	buf.WriteString(`<select onchange="if(this.value)location=this.value" data-searchable-select data-searchable-full-width data-search-placeholder="输入标签名筛选…" data-search-empty="没有匹配的标签">`)
	buf.WriteString(`<option value="">选择标签…</option>`)
	for _, t := range tags {
		buf.WriteString(`<option value="`)
		buf.WriteString(template.HTMLEscapeString(widgetTagURL(t.Slug)))
		buf.WriteString(`">`)
		buf.WriteString(template.HTMLEscapeString(t.Name))
		buf.WriteString(`</option>`)
	}
	buf.WriteString(`</select></section>`)
	return template.HTML(buf.String()), nil
}

func esc(s string) string {
	return template.HTMLEscapeString(s)
}
