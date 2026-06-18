package theme

import (
	"context"

	"github.com/youthlin/blog/internal/store"
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

// --- user_info ---

type userInfoWidget struct{}

func (w *userInfoWidget) Name() string { return "user_info" }

func (w *userInfoWidget) Data(ctx context.Context, st *store.Store, settings WidgetSettings) (any, error) {
	return UserInfoWidgetData{
		CurrentUserID:    settings.CurrentUserID,
		CurrentUserName:  settings.CurrentUserName,
		RegistrationOpen: settings.RegistrationOpen,
		CSRFToken:        settings.CSRFToken,
	}, nil
}

// --- search ---

type searchWidget struct{}

func (w *searchWidget) Name() string { return "search" }

func (w *searchWidget) Data(ctx context.Context, st *store.Store, settings WidgetSettings) (any, error) {
	return SearchWidgetData{Keyword: settings.Keyword}, nil
}

// --- saying (博主动态) ---

type sayingWidget struct{}

func (w *sayingWidget) Name() string { return "saying" }

func (w *sayingWidget) Data(ctx context.Context, st *store.Store, settings WidgetSettings) (any, error) {
	if settings.SayingPostID == 0 {
		return nil, nil
	}
	items := settings.SayingCommentItems
	if items == nil {
		items = st.SayingCommentItems(ctx, settings.SayingPostID, 5, 20)
	}
	if len(items) == 0 {
		return nil, nil
	}
	p := items[0].Post
	name := p.Author.DisplayName
	if name == "" {
		name = p.Author.Username
	}
	return SayingWidgetData{
		Items:         items,
		AuthorName:    name,
		AuthorEmail:   p.Author.Email,
		DefaultAvatar: settings.DefaultAvatar,
	}, nil
}

// --- recent_posts ---

type recentPostsWidget struct{}

func (w *recentPostsWidget) Name() string { return "recent_posts" }

func (w *recentPostsWidget) Data(ctx context.Context, st *store.Store, settings WidgetSettings) (any, error) {
	posts := settings.RecentPosts
	if posts == nil {
		posts = st.RecentPosts(ctx, 8)
	}
	if len(posts) == 0 {
		return nil, nil
	}
	return RecentPostsWidgetData{Posts: posts}, nil
}

// --- recent_comments ---

type recentCommentsWidget struct{}

func (w *recentCommentsWidget) Name() string { return "recent_comments" }

func (w *recentCommentsWidget) Data(ctx context.Context, st *store.Store, settings WidgetSettings) (any, error) {
	items := settings.RecentCommentItems
	if items == nil {
		items = st.RecentCommentItems(ctx, 8, 20)
	}
	if len(items) == 0 {
		return nil, nil
	}
	return RecentCommentsWidgetData{Items: items, DefaultAvatar: settings.DefaultAvatar}, nil
}

// --- archive_months ---

type archiveMonthsWidget struct{}

func (w *archiveMonthsWidget) Name() string { return "archive_months" }

func (w *archiveMonthsWidget) Data(ctx context.Context, st *store.Store, settings WidgetSettings) (any, error) {
	months := settings.ArchiveMonths
	if months == nil {
		months = st.ArchiveMonths(ctx)
	}
	if len(months) == 0 {
		return nil, nil
	}
	return ArchiveMonthsWidgetData{Months: months}, nil
}

// --- categories ---

type categoriesWidget struct{}

func (w *categoriesWidget) Name() string { return "categories" }

func (w *categoriesWidget) Data(ctx context.Context, st *store.Store, settings WidgetSettings) (any, error) {
	cats := settings.Categories
	if cats == nil {
		cats = st.AllCategories(ctx)
	}
	if len(cats) == 0 {
		return nil, nil
	}
	return CategoriesWidgetData{Categories: cats}, nil
}

// --- tags ---

type tagsWidget struct{}

func (w *tagsWidget) Name() string { return "tags" }

func (w *tagsWidget) Data(ctx context.Context, st *store.Store, settings WidgetSettings) (any, error) {
	tags := settings.Tags
	if tags == nil {
		tags = st.AllTags(ctx)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return TagsWidgetData{Tags: tags}, nil
}
