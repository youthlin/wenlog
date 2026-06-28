package script

import (
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

// CompileAndRegister 编译脚本源码，并调用脚本包中的 Register 函数。
func CompileAndRegister(source string, options CompileOptions) (*interp.Interpreter, error) {
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
	if _, err := i.Eval(source); err != nil {
		return nil, errors.Wrapf(err, "执行%s函数失败", subject)
	}

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
