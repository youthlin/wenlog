package plugin

import (
	"log/slog"
	"os"
	"reflect"

	"github.com/cockroachdb/errors"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	root "github.com/youthlin/blog/pluginapi"
)

// FunctionsScript 表示一个已编译的插件 functions 脚本。
type FunctionsScript struct {
	PluginID   string
	sourcePath string
	source     string
	api        *root.API
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
	i := interp.New(interp.Options{
		GoPath: os.Getenv("GOPATH"),
		Env:    os.Environ(),
		Stdout: nil,
		Stderr: nil,
	})
	if err := i.Use(stdlib.Symbols); err != nil {
		return nil, errors.Wrap(err, "准备插件运行环境失败")
	}
	if err := injectPluginAPI(i, api); err != nil {
		return nil, errors.Wrap(err, "注入插件API失败")
	}
	if _, err := i.Eval(source); err != nil {
		return nil, errors.Wrap(err, "执行插件函数失败")
	}
	registerFn, err := i.Eval("plugin.Register")
	if err != nil {
		return nil, errors.Wrap(err, "执行插件函数[plugin.Register]失败")
	}
	if registerFn.Kind() != reflect.Func {
		return nil, errors.New("插件函数中Register必须是函数类型")
	}
	if registerFn.Type().NumIn() != 0 {
		return nil, errors.New("插件函数中Register应当是无参函数")
	}
	registerFn.Call(nil)
	if log != nil {
		log.Info("插件函数functions.goyaegi编译执行成功",
			"plugin", pluginID,
			"path", path,
		)
	}
	return &FunctionsScript{PluginID: pluginID, sourcePath: path, source: source, api: api}, nil
}

func injectPluginAPI(i *interp.Interpreter, api *root.API) error {
	root.Api = api
	return i.Use(interp.Exports{
		"pluginapi/pluginapi": {
			"API":                 reflect.ValueOf((*root.API)(nil)),
			"ActionFunc":          reflect.ValueOf((*root.ActionFunc)(nil)),
			"FilterFunc":          reflect.ValueOf((*root.FilterFunc)(nil)),
			"WidgetRenderContext": reflect.ValueOf((*root.WidgetRenderContext)(nil)),
			"SayingItem":          reflect.ValueOf((*root.SayingItem)(nil)),
			"Api":                 reflect.ValueOf(root.Api),
			"PriorityEarly":       reflect.ValueOf(root.PriorityEarly),
			"PriorityDefault":     reflect.ValueOf(root.PriorityDefault),
			"PriorityLate":        reflect.ValueOf(root.PriorityLate),
		},
	})
}
