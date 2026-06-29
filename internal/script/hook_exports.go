package script

import (
	"reflect"

	"github.com/traefik/yaegi/interp"
	"github.com/youthlin/blog/hook"
)

// HookAPIExports 返回主题和插件脚本共用的 hook API yaegi 导出表。
// 保持唯一来源，避免 internal/plugin 与 internal/theme 各维护一份类型/常量清单。
// yaegi 的 Exports API 要求用 reflect.Value 描述宿主侧导出的类型、函数和常量，
// 这里的反射是解释器边界所必需，不参与业务运行时分发。
func HookAPIExports() interp.Exports {
	return interp.Exports{
		"hook/hook": {
			"API":                          reflect.ValueOf((*hook.API)(nil)),
			"Args":                         reflect.ValueOf((*hook.Args)(nil)),
			"ActionFunc":                   reflect.ValueOf((*hook.ActionFunc)(nil)),
			"FilterFunc":                   reflect.ValueOf((*hook.FilterFunc)(nil)),
			"Func":                         reflect.ValueOf((*hook.Func)(nil)),
			"PostView":                     reflect.ValueOf((*hook.PostView)(nil)),
			"CategoryView":                 reflect.ValueOf((*hook.CategoryView)(nil)),
			"TagView":                      reflect.ValueOf((*hook.TagView)(nil)),
			"CommentView":                  reflect.ValueOf((*hook.CommentView)(nil)),
			"UserView":                     reflect.ValueOf((*hook.UserView)(nil)),
			"ArchiveMonthView":             reflect.ValueOf((*hook.ArchiveMonthView)(nil)),
			"SelectOpt":                    reflect.ValueOf((*hook.SelectOpt)(nil)),
			"OptionDecl":                   reflect.ValueOf((*hook.OptionDecl)(nil)),
			"WidgetRenderContext":          reflect.ValueOf((*hook.WidgetRenderContext)(nil)),
			"HeadEndData":                  reflect.ValueOf((*hook.HeadEndData)(nil)),
			"BodyEndData":                  reflect.ValueOf((*hook.BodyEndData)(nil)),
			"CommentFormAfterTextareaData": reflect.ValueOf((*hook.CommentFormAfterTextareaData)(nil)),
			"PriorityEarly":                reflect.ValueOf(hook.PriorityEarly),
			"PriorityDefault":              reflect.ValueOf(hook.PriorityDefault),
			"PriorityLate":                 reflect.ValueOf(hook.PriorityLate),
			"HookHeadEnd":                  reflect.ValueOf(hook.HookHeadEnd),
			"HookBodyEnd":                  reflect.ValueOf(hook.HookBodyEnd),
			"HookCommentFormAfterTextarea": reflect.ValueOf(hook.HookCommentFormAfterTextarea),
			"HookWidgetRender":             reflect.ValueOf(hook.HookWidgetRender),
			"HookPostTitle":                reflect.ValueOf(hook.HookPostTitle),
			"HookPostExcerptHTML":          reflect.ValueOf(hook.HookPostExcerptHTML),
			"HookPostContentHTML":          reflect.ValueOf(hook.HookPostContentHTML),
			"HookCommentContentHTML":       reflect.ValueOf(hook.HookCommentContentHTML),
			"HookWidgetRenderHTML":         reflect.ValueOf(hook.HookWidgetRenderHTML),
		},
	}
}
