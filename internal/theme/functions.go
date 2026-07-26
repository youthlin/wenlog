package theme

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/render"
	internalscript "github.com/youthlin/wenlog/internal/script"
	"github.com/youthlin/wenlog/internal/store"
)

// FunctionsScript 表示一个已编译的 functions.go 脚本。
type FunctionsScript struct {
	source     string
	sourceHash string // 文件内容的简单 hash，用于检测变更
	api        *API
	funcs      map[string]hook.Func
	mu         sync.RWMutex
	pool       sync.Pool
}

// CompileFunctions 编译主题目录下的 functions.go 或 functions.goyaegi 文件。
// 返回编译后的脚本实例；如果文件不存在则返回 nil, nil。
func CompileFunctions(ctx context.Context, themeDir string, api *API, log *slog.Logger) (*FunctionsScript, error) {
	var rootAPI = api.API
	_, source, err := internalscript.CompileFromDir(ctx, themeDir, internalscript.CompileOptions{
		Subject:      "主题",
		PackageName:  "theme",
		Exports:      internalscript.HookAPIExports(),
		RegisterArgs: []any{rootAPI},
	})
	if err != nil {
		return nil, err
	}
	if source == "" {
		return nil, nil
	}
	if err := rootAPI.RegistrationError(); err != nil {
		return nil, err
	}
	return newFunctionsScript(ctx, source, api, log), nil
}

func newFunctionsScript(ctx context.Context, source string, api *API, log *slog.Logger) *FunctionsScript {
	hash := hashString(source)
	if log != nil {
		log.InfoContext(ctx, "主题函数functions.go编译执行成功",
			"funcs", api.FuncNames(),
		)
	}
	return &FunctionsScript{
		source:     source,
		sourceHash: hash,
		api:        api,
		funcs:      snapshotFuncs(api),
	}
}

func snapshotFuncs(api *API) map[string]hook.Func {
	if api == nil || api.API == nil {
		return nil
	}
	names := api.FuncNames()
	result := make(map[string]hook.Func, len(names))
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
	if script == nil || api == nil {
		return nil
	}
	return func(ctx *render.RequestContext, name string, args ...any) any {
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
		return callThemeFuncWithTimeout(renderContext(ctx), script, api, loader, name, parsed)
	}
}

func (script *FunctionsScript) acquireRunner(ctx context.Context, baseAPI *API, loader *store.DataLoader) (*FunctionsScript, error) {
	if v := script.pool.Get(); v != nil {
		runner, _ := v.(*FunctionsScript)
		if runner != nil && runner.api != nil {
			runner.api.WithLoader(loader)
			return runner, nil
		}
	}
	requestAPI := NewAPI(baseAPI.Domain(), loader, baseAPI.themeOptions)
	if _, err := internalscript.CompileAndRegister(ctx, script.source, internalscript.CompileOptions{
		Subject:      "主题",
		PackageName:  "theme",
		Exports:      internalscript.HookAPIExports(),
		RegisterArgs: []any{requestAPI.API},
	}); err != nil {
		return nil, err
	}
	if err := requestAPI.RegistrationError(); err != nil {
		return nil, err
	}
	return newFunctionsScript(ctx, script.source, requestAPI, nil), nil
}

func (script *FunctionsScript) releaseRunner(runner *FunctionsScript) {
	if script == nil || runner == nil || runner.api == nil {
		return
	}
	runner.api.WithLoader(nil)
	script.pool.Put(runner)
}

func callThemeFuncWithTimeout(ctx context.Context, script *FunctionsScript, baseAPI *API, loader *store.DataLoader, name string, args map[string]any) any {
	runner, err := script.acquireRunner(ctx, baseAPI, loader)
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

func safeCallThemeFunc(themeFunc hook.Func, api *hook.API, args map[string]any) (result any) {
	defer func() {
		if recover() != nil {
			result = nil
		}
	}()
	return themeFunc(api, args)
}
