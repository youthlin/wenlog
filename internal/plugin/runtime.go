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
func CompileFunctions(ctx context.Context, p *Plugin, hooks hook.Registry, log *slog.Logger) (*FunctionsScript, error) {
	if p == nil {
		return nil, nil
	}
	source := hook.Source{Type: hook.SourcePlugin, ID: p.ID}
	api := hook.NewAPI().
		WithRegistryBinding(hook.NewRegistryBinding(hooks, source)).
		WithDomain(p.PluginDomain())

	i, src, err := script.CompileFromDir(ctx, p.Dir, script.CompileOptions{
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
	if err := api.RegistrationError(); err != nil {
		return nil, err
	}
	if log != nil {
		log.InfoContext(ctx, "插件函数functions.goyaegi编译执行成功",
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
	_, err := script.CallOptionalFunc(ctx, s.interp, "plugin", name, "插件", []any{s.api.WithContext(ctx)})
	return err
}
