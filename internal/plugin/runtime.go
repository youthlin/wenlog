package plugin

import (
	"context"
	"log/slog"

	"github.com/traefik/yaegi/interp"
	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/script"
)

// FunctionsScript 表示一个已编译的插件 functions 脚本。
type FunctionsScript struct {
	PluginID string
	source   string
	api      *hook.API
	interp   *interp.Interpreter
}

// CompileFunctions 编译插件目录下的 functions.go 或 functions.goyaegi 文件。
// 如果插件没有 functions 文件，返回 nil, nil。
func CompileFunctions(p *Plugin, hooks *Registry, log *slog.Logger) (*FunctionsScript, error) {
	if p == nil {
		return nil, nil
	}
	source := Source{Type: SourcePlugin, ID: p.ID}
	api := hook.New(
		func(name string, fn any, priority ...int) { hooks.AddAction(name, fn, source, priority...) },
		func(name string, fn any, priority ...int) { hooks.AddFilter(name, fn, source, priority...) },
		p.PluginDomain(),
	)
	api.SetRemoveHooks(
		func(name string) { hooks.RemoveAction(name, source) },
		func(name string) { hooks.RemoveFilter(name, source) },
	)
	i, src, err := script.CompileFromDir(p.Dir, script.CompileOptions{
		Subject:      "插件",
		PackageName:  "plugin",
		Exports:      script.HookAPIExports(),
		RegisterArgs: []any{api},
	})
	if err != nil {
		return nil, err
	}
	if i == nil {
		return nil, nil
	}
	if log != nil {
		log.Info("插件函数functions.goyaegi编译执行成功",
			"plugin", p.ID,
		)
	}
	return &FunctionsScript{PluginID: p.ID, source: src, api: api, interp: i}, nil
}

// CallLifecycle 调用插件 functions 中可选的生命周期函数。
// 支持 Activate/Deactivate/Uninstall 这类签名：func(api *hook.API) error。
func (s *FunctionsScript) CallLifecycle(ctx context.Context, name string) error {
	if s == nil || s.interp == nil || s.api == nil || name == "" {
		return nil
	}
	_, err := script.CallOptionalFunc(s.interp, "plugin", name, "插件", []any{s.api.WithContext(ctx)})
	return err
}
