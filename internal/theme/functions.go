package theme

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
)

// FunctionsScript 表示一个已编译的 functions.go 脚本。
type FunctionsScript struct {
	sourcePath string
	sourceHash string // 文件内容的简单 hash，用于检测变更
	providers  map[string]DataProvider
	mu         sync.RWMutex
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
		return nil, fmt.Errorf("read functions script: %w", err)
	}

	source := string(data)
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
		return nil, fmt.Errorf("yaegi use stdlib: %w", err)
	}

	// 注入 ThemeAPI 类型和实例，让 functions.go 可以使用
	if err := injectThemeAPI(i, api); err != nil {
		return nil, fmt.Errorf("inject theme API: %w", err)
	}

	// 编译执行 functions.go
	if _, err := i.Eval(source); err != nil {
		return nil, fmt.Errorf("yaegi eval functions.go: %w", err)
	}

	// 查找并调用 Register 函数（无参数，Api 已通过 i.Use 注入为全局变量）
	registerFn, err := i.Eval("theme.Register")
	if err != nil {
		return nil, fmt.Errorf("functions.go must define Register(): %w", err)
	}

	// i.Eval 返回的 registerFn 已经是 reflect.Value
	if registerFn.Kind() != reflect.Func {
		return nil, fmt.Errorf("Register is not a function, got %s", registerFn.Kind())
	}

	if registerFn.Type().NumIn() != 0 {
		return nil, fmt.Errorf("Register must accept 0 arguments, got %d", registerFn.Type().NumIn())
	}

	registerFn.Call(nil)

	if log != nil {
		log.Info("functions.go compiled and registered",
			"path", path,
			"providers", api.ProviderNames(),
		)
	}

	return &FunctionsScript{
		sourcePath: path,
		sourceHash: hash,
		providers:  api.providers,
	}, nil
}

// injectThemeAPI 将 ThemeAPI 实例导出到 yaegi 解释器，使 functions.go 可以使用。
// 使用 i.Use 导出 Go 值，yaegi 通过反射调用其方法。
func injectThemeAPI(i *interp.Interpreter, api *API) error {
	// 先声明 View 类型，让 yaegi 知道这些类型
	typeDecl := `
package theme

import "time"

type PostView struct {
	ID           uint
	Title        string
	Slug         string
	Excerpt      string
	Content      string
	AuthorID     uint
	Status       string
	PostType     string
	Views        int64
	MenuOrder    int
	PublishedAt  time.Time
	ModifiedAt   time.Time
	CommentCount int64
	Author       UserView
	Categories   []CategoryView
	Tags         []TagView
}

type CategoryView struct {
	ID          uint
	Name        string
	Slug        string
	Description string
	ParentID    uint
	PostCount   int64
}

type TagView struct {
	ID        uint
	Name      string
	Slug      string
	PostCount int64
}

type CommentView struct {
	ID        uint
	PostID    uint
	ParentID  uint
	Author    string
	Content   string
	Status    string
	CreatedAt time.Time
}

type UserView struct {
	ID          uint
	Username    string
	DisplayName string
	Email       string
	Role        string
}

type ArchiveMonthView struct {
	Year  int
	Month int
	Count int64
}
`
	if _, err := i.Eval(typeDecl); err != nil {
		return fmt.Errorf("declare view types: %w", err)
	}

	// 使用 i.Use 导出 Go 的 *API 实例到 yaegi，避免类型不匹配
	// 导出到 "themeapi" 包，脚本通过 import "themeapi" 引用
	i.Use(interp.Exports{
		"themeapi/themeapi": {
			"Api": reflect.ValueOf(api),
		},
	})

	return nil
}

// hashString 返回字符串的简单 hash（用于检测文件变更）。
func hashString(s string) string {
	h := 0
	for _, c := range s {
		h = h*31 + int(c)
	}
	return fmt.Sprintf("%x", h)
}

// --- 模板函数 themeData ---

// themeDataFunc 返回一个可在模板中调用的 themeData 函数。
// 用法：{{themeData "provider_name" "key1" value1 "key2" value2}}
func themeDataFunc(script *FunctionsScript, api *API) func(name string, args ...any) any {
	return func(name string, args ...any) any {
		if script == nil || api == nil {
			return nil
		}
		script.mu.RLock()
		provider := script.providers[name]
		script.mu.RUnlock()
		if provider == nil {
			return nil
		}

		// 解析 key-value 参数对
		parsed := parseKVArgs(args)
		loader, _ := render.CurrentThemeLoader().(*store.DataLoader)
		return callProviderWithTimeout(api, provider, loader, parsed)
	}
}

func callProviderWithTimeout(api *API, provider DataProvider, loader *store.DataLoader, args map[string]any) any {
	if !api.TryBeginProvider(loader) {
		return nil
	}
	released := make(chan struct{})
	done := make(chan any, 1)
	go func() {
		defer func() {
			api.EndProvider()
			close(released)
		}()
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
