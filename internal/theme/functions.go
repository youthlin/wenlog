package theme

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	root "github.com/youthlin/blog/themeapi"
)

// FunctionsScript 表示一个已编译的 functions.go 脚本。
type FunctionsScript struct {
	sourcePath string
	source     string
	sourceHash string // 文件内容的简单 hash，用于检测变更
	api        *API
	funcs      map[string]DataProvider
	mu         sync.RWMutex
	pool       sync.Pool
}

// CompileFunctions 编译主题目录下的 functions.go 或 functions.goyaegi 文件。
// 返回编译后的脚本实例；如果文件不存在则返回 nil, nil。
func CompileFunctions(themeDir string, api *API, log *slog.Logger) (*FunctionsScript, error) {
	// 优先 functions.go，其次 functions.goyaegi（embed 中用 .goyaegi 避免被 Go 编译器处理）
	path := ""
	for _, name := range []string{"functions.go", "functions.goyaegi"} {
		candidate := filepath.Join(themeDir, name)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
			break
		}
	}
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "读取主题函数文件失败: %s", path)
	}

	return compileFunctionsSource(path, string(data), api, log)
}

func compileFunctionsSource(path, source string, api *API, log *slog.Logger) (*FunctionsScript, error) {
	hash := hashString(source)

	// 创建 yaegi 解释器，使用白名单限制可用包
	i := interp.New(interp.Options{
		GoPath: os.Getenv("GOPATH"),
		Env:    os.Environ(),
		Stdout: nil,
		Stderr: nil,
	})

	// 只加载安全的符号表
	if err := i.Use(stdlib.Symbols); err != nil {
		return nil, errors.Wrapf(err, "准备插件环境失败")
	}

	// 注入 ThemeAPI 类型和实例，让 functions.go 可以使用
	if err := injectThemeAPI(i, api); err != nil {
		return nil, errors.Wrapf(err, "注入主题API失败")
	}

	// 编译执行 functions.go
	if _, err := i.Eval(source); err != nil {
		return nil, errors.Wrapf(err, "执行主题函数失败")
	}

	// 查找并调用 Register 函数（无参数，Api 已通过 i.Use 注入为全局变量）
	registerFn, err := i.Eval("theme.Register")
	if err != nil {
		return nil, errors.Wrapf(err, "执行主题函数[theme.Register]失败")
	}

	// i.Eval 返回的 registerFn 已经是 reflect.Value
	if registerFn.Kind() != reflect.Func {
		return nil, errors.Errorf("主题函数中Register必须是函数类型")
	}

	if registerFn.Type().NumIn() != 0 {
		return nil, errors.Errorf("主题函数中Register应当是无参函数")
	}

	registerFn.Call(nil)

	if log != nil {
		log.Info("主题函数functions.go编译执行成功",
			"path", path,
			"funcs", api.FuncNames(),
		)
	}

	return &FunctionsScript{
		sourcePath: path,
		source:     source,
		sourceHash: hash,
		api:        api,
		funcs:      snapshotFuncs(api),
	}, nil
}

func snapshotFuncs(api *API) map[string]DataProvider {
	if api == nil || api.API == nil {
		return nil
	}
	names := api.FuncNames()
	result := make(map[string]DataProvider, len(names))
	for _, name := range names {
		result[name] = api.GetFunc(name)
	}
	return result
}

// injectThemeAPI 将 ThemeAPI 实例导出到 yaegi 解释器，使 functions.go 可以使用。
// 使用 i.Use 导出 Go 值，yaegi 通过反射调用其方法。
func injectThemeAPI(i *interp.Interpreter, api *API) error {
	if api != nil {
		root.Api = api.API
	}
	// 使用 i.Use 导出 Go 的 API 实例和 View 类型到 yaegi，避免维护一份重复的结构体声明。
	// 导出到 "themeapi" 包，脚本通过 import "themeapi" 引用。
	return i.Use(interp.Exports{
		"themeapi/themeapi": {
			"API":              reflect.ValueOf((*root.API)(nil)),
			"Api":              reflect.ValueOf(root.Api),
			"PostView":         reflect.ValueOf((*root.PostView)(nil)),
			"CategoryView":     reflect.ValueOf((*root.CategoryView)(nil)),
			"TagView":          reflect.ValueOf((*root.TagView)(nil)),
			"CommentView":      reflect.ValueOf((*root.CommentView)(nil)),
			"UserView":         reflect.ValueOf((*root.UserView)(nil)),
			"ArchiveMonthView": reflect.ValueOf((*root.ArchiveMonthView)(nil)),
			"SayingItem":       reflect.ValueOf((*root.SayingItem)(nil)),
		},
	})
}

// hashString 返回字符串的简单 hash（用于检测文件变更）。
func hashString(s string) string {
	h := 0
	for _, c := range s {
		h = h*31 + int(c)
	}
	return fmt.Sprintf("%x", h)
}

// --- 模板函数 themeInvoke ---

// themeInvokeFunc 返回一个可在模板中调用的 themeInvoke 函数。
// 用法：{{themeInvoke "func_name" "key1" value1 "key2" value2}}
func themeInvokeFunc(script *FunctionsScript, api *API) func(ctx *render.RequestContext, name string, args ...any) any {
	return func(ctx *render.RequestContext, name string, args ...any) any {
		if script == nil || api == nil {
			return nil
		}
		script.mu.RLock()
		_, exists := script.funcs[name]
		script.mu.RUnlock()
		if !exists {
			return nil
		}

		// 解析 key-value 参数对
		parsed := parseKVArgs(args)
		var loader *store.DataLoader
		if ctx != nil {
			loader, _ = ctx.ThemeLoader.(*store.DataLoader)
		}
		return callProviderWithTimeout(script, api, loader, name, parsed)
	}
}

func (script *FunctionsScript) acquireRunner(baseAPI *API, loader *store.DataLoader) (*FunctionsScript, error) {
	if v := script.pool.Get(); v != nil {
		runner, _ := v.(*FunctionsScript)
		if runner != nil && runner.api != nil {
			runner.api.SetLoader(loader)
			return runner, nil
		}
	}
	requestAPI := NewAPI(loader)
	requestAPI.SetThemeOptions(baseAPI.themeOptions)
	return compileFunctionsSource(script.sourcePath, script.source, requestAPI, nil)
}

func (script *FunctionsScript) releaseRunner(runner *FunctionsScript) {
	if script == nil || runner == nil || runner.api == nil {
		return
	}
	runner.api.SetLoader(nil)
	script.pool.Put(runner)
}

func callProviderWithTimeout(script *FunctionsScript, baseAPI *API, loader *store.DataLoader, name string, args map[string]any) any {
	runner, err := script.acquireRunner(baseAPI, loader)
	if err != nil || runner == nil {
		return nil
	}
	provider := runner.funcs[name]
	if provider == nil {
		script.releaseRunner(runner)
		return nil
	}
	done := make(chan any, 1)
	go func() {
		defer script.releaseRunner(runner)
		done <- safeCallProvider(provider, args)
	}()

	select {
	case result := <-done:
		return result
	case <-time.After(time.Second):
		return nil
	}
}

func safeCallProvider(provider DataProvider, args map[string]any) (result any) {
	defer func() {
		if recover() != nil {
			result = nil
		}
	}()
	return provider(args)
}

// parseKVArgs 解析 key-value 参数对为 map[string]any。
// 例如 ("n", 5, "tag", "go") → {"n": 5, "tag": "go"}
func parseKVArgs(args []any) map[string]any {
	result := make(map[string]any, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		result[key] = args[i+1]
	}
	return result
}
