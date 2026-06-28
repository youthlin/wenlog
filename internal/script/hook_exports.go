package script

import (
	"reflect"

	"github.com/traefik/yaegi/interp"
	root "github.com/youthlin/blog/hook"
)

// HookAPIExports 返回主题和插件脚本共用的 hook API yaegi 导出表。
// 保持唯一来源，避免 internal/plugin 与 internal/theme 各维护一份类型/常量清单。
func HookAPIExports() interp.Exports {
	return interp.Exports{
		"hook/hook": {
			"API":                          reflect.ValueOf((*root.API)(nil)),
			"ActionFunc":                   reflect.ValueOf((*root.ActionFunc)(nil)),
			"FilterFunc":                   reflect.ValueOf((*root.FilterFunc)(nil)),
			"Func":                         reflect.ValueOf((*root.Func)(nil)),
			"PostView":                     reflect.ValueOf((*root.PostView)(nil)),
			"CategoryView":                 reflect.ValueOf((*root.CategoryView)(nil)),
			"TagView":                      reflect.ValueOf((*root.TagView)(nil)),
			"CommentView":                  reflect.ValueOf((*root.CommentView)(nil)),
			"UserView":                     reflect.ValueOf((*root.UserView)(nil)),
			"ArchiveMonthView":             reflect.ValueOf((*root.ArchiveMonthView)(nil)),
			"SelectOpt":                    reflect.ValueOf((*root.SelectOpt)(nil)),
			"OptionDecl":                   reflect.ValueOf((*root.OptionDecl)(nil)),
			"WidgetRenderContext":          reflect.ValueOf((*root.WidgetRenderContext)(nil)),
			"PriorityEarly":                reflect.ValueOf(root.PriorityEarly),
			"PriorityDefault":              reflect.ValueOf(root.PriorityDefault),
			"PriorityLate":                 reflect.ValueOf(root.PriorityLate),
			"HookHeadEnd":                  reflect.ValueOf(root.HookHeadEnd),
			"HookBodyEnd":                  reflect.ValueOf(root.HookBodyEnd),
			"HookCommentFormAfterTextarea": reflect.ValueOf(root.HookCommentFormAfterTextarea),
			"HookWidgetRender":             reflect.ValueOf(root.HookWidgetRender),
			"HookPostTitle":                reflect.ValueOf(root.HookPostTitle),
			"HookPostExcerptHTML":          reflect.ValueOf(root.HookPostExcerptHTML),
			"HookPostContentHTML":          reflect.ValueOf(root.HookPostContentHTML),
			"HookCommentContentHTML":       reflect.ValueOf(root.HookCommentContentHTML),
			"HookWidgetRenderHTML":         reflect.ValueOf(root.HookWidgetRenderHTML),
		},
	}
}
