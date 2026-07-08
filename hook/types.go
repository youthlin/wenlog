package hook

import (
	"context"
	"html/template"
	"strconv"
	"strings"
)

// ActionFunc 是插件注册 action 时推荐使用的函数签名。
type ActionFunc = func(api *API, args ...any)

// FilterFunc 是插件注册 filter 时推荐使用的函数签名。
type FilterFunc = func(api *API, value any, args ...any) any

// Args 是模板通过 hook_invoke 传给扩展函数的命名参数。
//
// 主题/插件作者可以直接用：
//
//	api.RegisterFunc("latest", func(api *hook.API, args hook.Args) any {
//		return api.RecentPosts(args.Int("count", 5))
//	})
//
// 比起在每个扩展里重复写类型断言，这些小工具让 functions.goyaegi 的主流程更接近业务描述。
type Args map[string]any

// Any 返回原始参数值；不存在时返回 nil。
func (a Args) Any(key string) any { return a[key] }

// String 读取字符串参数；空字符串或不存在时返回 def。
func (a Args) String(key, def string) string {
	if s, ok := a[key].(string); ok && s != "" {
		return s
	}
	return def
}

// Int 读取整数参数；支持常见整型、浮点数和数字字符串。
func (a Args) Int(key string, def int) int {
	switch v := a[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case uint:
		return int(v)
	case uint64:
		return int(v)
	case uint32:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// PositiveInt 读取正整数参数；不存在、无法转换或小于等于 0 时返回 def。
func (a Args) PositiveInt(key string, def int) int {
	if n := a.Int(key, def); n > 0 {
		return n
	}
	return def
}

// Bool 读取布尔参数；支持 bool 和 strconv.ParseBool 可识别的字符串。
func (a Args) Bool(key string, def bool) bool {
	switch v := a[key].(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

// Func 是插件注册给模板调用的数据函数。
type Func = func(api *API, args Args) any

// WidgetRenderContext 是 widget.render action 接收的插件组件渲染上下文。
type WidgetRenderContext struct {
	PluginID string
	WidgetID string
	Options  map[string]string
	Data     any
}

// SelectOpt 是 select 类型选项的一个可选项。
type SelectOpt struct {
	Value string `yaml:"value" json:"value"`
	Label string `yaml:"label" json:"label"`
}

// Source 记录一个 hook 处理器来自核心、主题还是插件。
type Source struct {
	Type string // core / theme / plugin
	ID   string // default / twentytwenty / saying
}

// WidgetDecl 描述主题或插件声明的一个可用组件。
type WidgetDecl struct {
	ID       string       `yaml:"id" json:"id"`
	Label    string       `yaml:"label" json:"label"`
	Options  []OptionDecl `yaml:"options,omitempty" json:"options,omitempty"`
	Source   string       `yaml:"-" json:"source,omitempty"`
	PluginID string       `yaml:"-" json:"plugin_id,omitempty"`
}
type WidgetDeclProvider = func(ctx context.Context) []WidgetDecl

// WidgetInstance 表示一个组件实例的运行时状态，包含实例 ID 和配置值。
// 同一组件类型可以在同一区域添加多次，每次有独立的 InstanceID 和 Settings。
type WidgetInstance struct {
	InstanceID string            // 实例唯一 ID
	WidgetID   string            // 组件类型 ID
	Settings   map[string]string // 实例级配置值
}

// WidgetResolver 根据来源和 ID 查找组件实现。
// source 为 "builtin" / "theme" / "plugin"，pluginID 仅在 source="plugin" 时有值
type WidgetResolver = func(source, id, pluginID string) Widget

// Widget 是所有组件（内置/主题/插件）的统一接口。
// 参照 WordPress WP_Widget 的模板方法模式：核心管理生命周期和存储，
// 组件实现者只需提供元数据和渲染逻辑。
type Widget interface {
	// Meta 返回组件声明元数据（ID、标签、可配置选项等）。
	Meta() WidgetDecl
	// Render 渲染组件为 HTML。tpl 是当前请求的主题模板实例，模板型组件用它执行模板；
	// action 型组件可忽略 tpl 参数。
	Render(ctx context.Context, tpl *template.Template, instance WidgetInstance, data any) (template.HTML, error)
}

// SettingStore 是主题/插件管理器需要的设置存储接口。
type SettingStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

// Translatable 表示可加载翻译的资源（主题或插件）。
type Translatable interface {
	LoadTranslations() error
}

// LoadTranslations 遍历列表加载所有资源的翻译。
func LoadTranslations[T Translatable](items []T) error {
	for _, item := range items {
		if err := item.LoadTranslations(); err != nil {
			return err
		}
	}
	return nil
}

// OptionDecl 描述主题或扩展声明的一个可配置选项。
type OptionDecl struct {
	ID          string      `yaml:"id" json:"id"`
	Type        string      `yaml:"type" json:"type"`
	Label       string      `yaml:"label" json:"label"`
	Description string      `yaml:"description" json:"description"`
	Default     string      `yaml:"default" json:"default"`
	Min         *float64    `yaml:"min" json:"min,omitempty"`
	Max         *float64    `yaml:"max" json:"max,omitempty"`
	Options     []SelectOpt `yaml:"options" json:"options,omitempty"`
}
