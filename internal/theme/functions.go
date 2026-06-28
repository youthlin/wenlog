package theme

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	root "github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/render"
	internalscript "github.com/youthlin/blog/internal/script"
	"github.com/youthlin/blog/internal/store"
)

// FunctionsScript 表示一个已编译的 functions.go 脚本。
type FunctionsScript struct {
	sourcePath string
	source     string
	sourceHash string // 文件内容的简单 hash，用于检测变更
	api        *API
	funcs      map[string]ThemeFunc
	mu         sync.RWMutex
	pool       sync.Pool
}

// CompileFunctions 编译主题目录下的 functions.go 或 functions.goyaegi 文件。
// 返回编译后的脚本实例；如果文件不存在则返回 nil, nil。
func CompileFunctions(themeDir string, api *API, log *slog.Logger) (*FunctionsScript, error) {
	// 查找 function.go[yaegi] 文件
	path := internalscript.FindFunctionsPath(themeDir)
	if path == "" {
		return nil, nil
	}
	// 读取文件内容
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "读取主题函数文件失败: %s", path)
	}
	// 编译
	return compileFunctionsSource(path, string(data), api, log)
}

func compileFunctionsSource(path, source string, api *API, log *slog.Logger) (*FunctionsScript, error) {
	hash := hashString(source)
	var rootAPI *root.API
	if api != nil {
		rootAPI = api.API
	}

	if _, err := internalscript.CompileAndRegister(source, internalscript.CompileOptions{
		Subject:      "主题",
		PackageName:  "theme",
		Exports:      internalscript.HookAPIExports(),
		RegisterArgs: []any{rootAPI},
	}); err != nil {
		return nil, err
	}

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

func snapshotFuncs(api *API) map[string]ThemeFunc {
	if api == nil || api.API == nil {
		return nil
	}
	names := api.FuncNames()
	result := make(map[string]ThemeFunc, len(names))
	for _, name := range names {
		result[name] = api.GetFunc(name)
	}
	return result
}

// hashString 返回字符串的简单 hash（用于检测文件变更）。
func hashString(s string) string {
	h := 0
	for _, c := range s {
		h = h*31 + int(c)
	}
	return fmt.Sprintf("%x", h)
}

// --- 模板函数 hookInvoke ---

// hookInvokeFunc 返回一个可在模板中调用的 hookInvoke 函数。
// 用法：{{hookInvoke "func_name" "key1" value1 "key2" value2}}
func hookInvokeFunc(script *FunctionsScript, api *API) func(ctx *render.RequestContext, name string, args ...any) any {
	if script == nil {
		return nil
	}
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

		parsed := internalscript.ParseKVArgs(args)
		var loader *store.DataLoader
		if ctx != nil {
			loader, _ = ctx.ThemeLoader.(*store.DataLoader)
		}
		return callThemeFuncWithTimeout(script, api, loader, name, parsed)
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

func callThemeFuncWithTimeout(script *FunctionsScript, baseAPI *API, loader *store.DataLoader, name string, args map[string]any) any {
	runner, err := script.acquireRunner(baseAPI, loader)
	if err != nil || runner == nil {
		return nil
	}
	themeFunc := runner.funcs[name]
	if themeFunc == nil {
		script.releaseRunner(runner)
		return nil
	}
	done := make(chan any, 1)
	go func() {
		defer script.releaseRunner(runner)
		done <- safeCallThemeFunc(themeFunc, runner.api.API, args)
	}()

	select {
	case result := <-done:
		return result
	case <-time.After(time.Second):
		return nil
	}
}

func safeCallThemeFunc(themeFunc ThemeFunc, api *root.API, args map[string]any) (result any) {
	defer func() {
		if recover() != nil {
			result = nil
		}
	}()
	return themeFunc(api, args)
}
