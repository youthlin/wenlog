package theme

import (
	"github.com/youthlin/blog/internal/store"
	root "github.com/youthlin/blog/themeapi"
)

type ThemeFunc = root.Func
type PostView = root.PostView
type CategoryView = root.CategoryView
type TagView = root.TagView
type CommentView = root.CommentView
type UserView = root.UserView
type ArchiveMonthView = root.ArchiveMonthView
type SayingItem = root.SayingItem

// API 是 themeapi.API 在 internal/theme 中的包装，负责桥接内部主题声明类型。
type API struct {
	*root.API
	themeOptions []OptionDecl
}

// NewAPI 创建 ThemeAPI 实例。
func NewAPI(loader *store.DataLoader) *API {
	return &API{API: root.New(loader)}
}

// SetThemeOptions 设置主题声明的选项列表（含默认值），供 GetOption 回退使用。
func (api *API) SetThemeOptions(opts []OptionDecl) {
	if api == nil || api.API == nil {
		return
	}
	api.themeOptions = append(api.themeOptions[:0], opts...)
	converted := make([]root.OptionDecl, 0, len(opts))
	for _, opt := range opts {
		converted = append(converted, root.OptionDecl{
			ID:          opt.ID,
			Type:        opt.Type,
			Label:       opt.Label,
			Description: opt.Description,
			Default:     opt.Default,
			Min:         opt.Min,
			Max:         opt.Max,
			Options:     convertSelectOpts(opt.Options),
		})
	}
	api.API.SetThemeOptions(converted)
}

func convertSelectOpts(opts []SelectOpt) []root.SelectOpt {
	if len(opts) == 0 {
		return nil
	}
	out := make([]root.SelectOpt, 0, len(opts))
	for _, opt := range opts {
		out = append(out, root.SelectOpt{Value: opt.Value, Label: opt.Label})
	}
	return out
}
