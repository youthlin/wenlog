package plugin

import (
	"context"
	"log/slog"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/traefik/yaegi/interp"
	root "github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/script"
)

// FunctionsScript 表示一个已编译的插件 functions 脚本。
type FunctionsScript struct {
	PluginID   string
	sourcePath string
	source     string
	api        *root.API
	interp     *interp.Interpreter
}

// CompileFunctions 编译插件目录下的 functions.go 或 functions.goyaegi 文件。
// 如果插件没有 functions 文件，返回 nil, nil。
func CompileFunctions(p *Plugin, hooks *Registry, log *slog.Logger) (*FunctionsScript, error) {
	if p == nil {
		return nil, nil
	}
	path := p.FunctionsPath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "读取插件函数文件失败: %s", path)
	}
	source := Source{Type: SourcePlugin, ID: p.ID}
	api := root.New(
		func(name string, fn any, priority ...int) { hooks.AddAction(name, fn, source, priority...) },
		func(name string, fn any, priority ...int) { hooks.AddFilter(name, fn, source, priority...) },
		p.PluginDomain(),
	)
	return compileFunctionsSource(p.ID, path, string(data), api, log)
}

func compileFunctionsSource(pluginID, path, source string, api *root.API, log *slog.Logger) (*FunctionsScript, error) {
	i, err := script.CompileAndRegister(source, script.CompileOptions{
		Subject:      "插件",
		PackageName:  "plugin",
		Exports:      script.HookAPIExports(),
		RegisterArgs: []any{api},
	})
	if err != nil {
		return nil, err
	}
	if log != nil {
		log.Info("插件函数functions.goyaegi编译执行成功",
			"plugin", pluginID,
			"path", path,
		)
	}
	return &FunctionsScript{PluginID: pluginID, sourcePath: path, source: source, api: api, interp: i}, nil
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
