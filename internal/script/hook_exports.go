package script

import (
	"reflect"

	"github.com/traefik/yaegi/interp"
	"github.com/youthlin/wenlog/hook"
)

// HookAPIExports 返回主题和插件脚本共用的 hook API yaegi 导出表。
// 保持唯一来源，避免 internal/plugin 与 internal/theme 各维护一份类型/常量清单。
// yaegi 的 Exports API 要求用 reflect.Value 描述宿主侧导出的类型、函数和常量，
// 这里的反射是解释器边界所必需，不参与业务运行时分发。
func HookAPIExports() interp.Exports {
	return interp.Exports{
		"hook/hook": {
			"API":                 reflect.ValueOf((*hook.API)(nil)),
			"Args":                reflect.ValueOf((*hook.Args)(nil)),
			"PostView":            reflect.ValueOf((*hook.PostView)(nil)),
			"CategoryView":        reflect.ValueOf((*hook.CategoryView)(nil)),
			"TagView":             reflect.ValueOf((*hook.TagView)(nil)),
			"CommentView":         reflect.ValueOf((*hook.CommentView)(nil)),
			"UserView":            reflect.ValueOf((*hook.UserView)(nil)),
			"ArchiveMonthView":    reflect.ValueOf((*hook.ArchiveMonthView)(nil)),
			"SelectOpt":           reflect.ValueOf((*hook.SelectOpt)(nil)),
			"OptionDecl":          reflect.ValueOf((*hook.OptionDecl)(nil)),
			"WidgetRenderContext": reflect.ValueOf((*hook.WidgetRenderContext)(nil)),
			// Consts 聚合所有 hook 名称常量和优先级值，新增 hook 时只需修改 hook.Consts 而无需逐个导出。
			"Consts": reflect.ValueOf(hook.Consts),
		},
	}
}
