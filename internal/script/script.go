package script

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"

	"github.com/cockroachdb/errors"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

var functionsFileNames = []string{"functions.go", "functions.goyaegi"}

// FindFunctionsPath 返回扩展目录下优先匹配的 functions 脚本路径。
// 主题和插件共用同一套查找规则：优先 functions.go，其次 functions.goyaegi。
func FindFunctionsPath(dir string) string {
	if dir == "" {
		return ""
	}
	for _, name := range functionsFileNames {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// CompileOptions 描述一段 yaegi 脚本的编译与 Register 调用参数。
type CompileOptions struct {
	Subject      string
	PackageName  string
	Exports      interp.Exports
	RegisterArgs []any
}

// CompileFromDir 从目录中查找并编译 functions 脚本，返回解释器实例和源码内容。
// 如果目录中没有 functions 文件，返回 nil, "", nil。
func CompileFromDir(ctx context.Context, dir string, options CompileOptions) (*interp.Interpreter, string, error) {
	path := FindFunctionsPath(dir)
	if path == "" {
		return nil, "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", errors.Wrapf(err, "读取%s函数文件失败: %s", options.Subject, path)
	}
	source := string(data)
	i, err := CompileAndRegister(ctx, source, options)
	if err != nil {
		return nil, "", err
	}
	return i, source, nil
}

// CompileAndRegister 编译脚本源码，并调用脚本包中的 Register 函数。
func CompileAndRegister(ctx context.Context, source string, options CompileOptions) (*interp.Interpreter, error) {
	subject := options.Subject
	if subject == "" {
		subject = "脚本"
	}
	if options.PackageName == "" {
		return nil, errors.Errorf("%s包名不能为空", subject)
	}

	i := interp.New(interp.Options{
		GoPath: os.Getenv("GOPATH"),
		Env:    os.Environ(),
		Stdout: nil,
		Stderr: nil,
	})
	if err := i.Use(stdlib.Symbols); err != nil {
		return nil, errors.Wrapf(err, "准备%s运行环境失败", subject)
	}
	if options.Exports != nil {
		if err := i.Use(options.Exports); err != nil {
			return nil, errors.Wrapf(err, "注入%sAPI失败", subject)
		}
	}
	prog, err := safeEval(ctx, i, source)
	if err != nil {
		return nil, errors.Wrapf(err, "执行%s函数失败", subject)
	}
	_ = prog

	registerName := options.PackageName + ".Register"
	registerFn, err := i.Eval(registerName)
	if err != nil {
		return nil, errors.Wrapf(err, "执行%s函数[%s]失败", subject, registerName)
	}
	if registerFn.Kind() != reflect.Func {
		return nil, errors.Errorf("%s函数中Register必须是函数类型", subject)
	}
	if err := validateRegisterArgs(subject, registerFn, options.RegisterArgs); err != nil {
		return nil, err
	}
	if err := callRegister(subject, registerFn, options.RegisterArgs); err != nil {
		return nil, err
	}
	return i, nil
}

// safeEval 调用 i.Eval，捕获 yaegi 内部可能的 panic（如 Go 版本不兼容）。
func safeEval(ctx context.Context, i *interp.Interpreter, source string) (prog any, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		if r := recover(); r != nil {
			err = errors.Errorf("yaegi 编译脚本时 panic: %v", r)
			slog.ErrorContext(ctx, "执行脚本panic", slog.Any("error", err))
		}
	}()
	v, evalErr := i.Eval(source)
	return v, evalErr
}

// 以下 helper 都在 yaegi 解释器边界工作：Eval 返回的是 reflect.Value，
// Register/Activate 等脚本入口也只能通过 reflect.Call 调用。因此这里的反射不可避免，
// 但范围限制在脚本加载/生命周期调用阶段，不进入普通模板渲染主流程。

// CallOptionalFunc 调用脚本包中的可选函数；函数不存在时返回 false, nil。
// 生命周期函数复用 Register 的参数校验规则，并允许函数返回 error。
func CallOptionalFunc(i *interp.Interpreter, packageName, funcName, subject string, args []any) (bool, error) {
	if i == nil || packageName == "" || funcName == "" {
		return false, nil
	}
	if subject == "" {
		subject = "脚本"
	}
	fullName := packageName + "." + funcName
	fn, err := i.Eval(fullName)
	if err != nil {
		return false, nil
	}
	if fn.Kind() != reflect.Func {
		return true, errors.Errorf("%s函数中%s必须是函数类型", subject, funcName)
	}
	if err := validateRegisterArgs(subject, fn, args); err != nil {
		return true, err
	}
	outs, err := callFunction(subject, funcName, fn, args)
	if err != nil {
		return true, err
	}
	if len(outs) == 0 {
		return true, nil
	}
	if len(outs) > 1 {
		return true, errors.Errorf("%s函数中%s最多只能返回一个error", subject, funcName)
	}
	if !outs[0].IsValid() {
		return true, nil
	}
	if isNilValue(outs[0]) {
		return true, nil
	}
	errValue, ok := outs[0].Interface().(error)
	if !ok {
		return true, errors.Errorf("%s函数中%s返回值必须是error", subject, funcName)
	}
	return true, errValue
}

func isNilValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func validateRegisterArgs(subject string, registerFn reflect.Value, args []any) error {
	fnType := registerFn.Type()
	if fnType.NumIn() != len(args) {
		return errors.Errorf("%s函数中Register参数数量应当为%d个", subject, len(args))
	}
	for i, arg := range args {
		if arg == nil {
			continue
		}
		argType := reflect.TypeOf(arg)
		wantType := fnType.In(i)
		if !argType.AssignableTo(wantType) && !argType.ConvertibleTo(wantType) {
			return errors.Errorf("%s函数中Register第%d个参数类型应当为%s", subject, i+1, wantType)
		}
	}
	return nil
}

func callRegister(subject string, registerFn reflect.Value, args []any) (err error) {
	_, err = callFunction(subject, "Register", registerFn, args)
	return err
}

func callFunction(subject, name string, fn reflect.Value, args []any) (outs []reflect.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.Errorf("%s函数%s执行panic: %v", subject, name, r)
		}
	}()
	in := make([]reflect.Value, 0, len(args))
	fnType := fn.Type()
	for i, arg := range args {
		wantType := fnType.In(i)
		if arg == nil {
			in = append(in, reflect.Zero(wantType))
			continue
		}
		v := reflect.ValueOf(arg)
		if v.Type().ConvertibleTo(wantType) && !v.Type().AssignableTo(wantType) {
			v = v.Convert(wantType)
		}
		in = append(in, v)
	}
	return fn.Call(in), nil
}

// ParseKVArgs 解析 key-value 参数对为 map[string]any。
// 例如 ("n", 5, "tag", "go") → {"n": 5, "tag": "go"}。
func ParseKVArgs(args []any) map[string]any {
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
