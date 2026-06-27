package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/i18n"
	"gopkg.in/yaml.v3"
)

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Plugin 表示一个已安装插件的 manifest 信息。
type Plugin struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description" json:"description"`
	Author      string `yaml:"author" json:"author"`
	// DefaultEnabled 表示尚未保存启用列表时是否默认启用该插件。
	DefaultEnabled bool         `yaml:"default_enabled" json:"default_enabled"`
	Dir            string       `yaml:"-" json:"-"`
	Hooks          HookDecl     `yaml:"hooks" json:"hooks"`
	Widgets        []WidgetDecl `yaml:"widgets" json:"widgets"`
	Settings       []OptionDecl `yaml:"settings" json:"settings"`
	Assets         AssetDecl    `yaml:"assets" json:"assets"`
}

// HookDecl 描述插件声明会使用的标准 hook/slot。
// 该声明主要用于后台展示和能力说明，实际注册以 functions.goyaegi 为准。
type HookDecl struct {
	Actions []string `yaml:"actions" json:"actions"`
	Filters []string `yaml:"filters" json:"filters"`
	Slots   []string `yaml:"slots" json:"slots"`
}

// WidgetDecl 描述插件提供的一类组件。
type WidgetDecl struct {
	ID      string       `yaml:"id" json:"id"`
	Label   string       `yaml:"label" json:"label"`
	Options []OptionDecl `yaml:"options" json:"options"`
	Source  string       `yaml:"-" json:"source"`
}

// OptionDecl 描述插件全局选项或组件实例选项。
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

// SelectOpt 是 select 类型选项的一个可选项。
type SelectOpt struct {
	Value string `yaml:"value" json:"value"`
	Label string `yaml:"label" json:"label"`
}

// AssetDecl 描述插件静态资源目录。
type AssetDecl struct {
	Dir string `yaml:"dir" json:"dir"`
}

// LoadPlugin 从目录加载 plugin.yaml 并返回 Plugin。
func LoadPlugin(dir string) (*Plugin, error) {
	path := filepath.Join(dir, "plugin.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "读取[%s]中的插件描述文件(plugin.yaml)失败", dir)
	}
	var p Plugin
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, errors.Wrapf(err, "解析[%s]中的插件描述文件(plugin.yaml)失败", dir)
	}
	if err := validatePluginManifest(dir, &p); err != nil {
		return nil, err
	}
	p.Dir = dir
	for i := range p.Widgets {
		p.Widgets[i].Source = "plugin:" + p.ID
	}
	return &p, nil
}

func validatePluginManifest(dir string, p *Plugin) error {
	if p == nil {
		return errors.New("插件描述文件(plugin.yaml)为空")
	}
	if p.ID == "" {
		return errors.New("插件描述文件(plugin.yaml)中缺少插件 ID")
	}
	if !pluginIDPattern.MatchString(p.ID) {
		return errors.Errorf("插件 ID 只能包含小写字母、数字、下划线和短横线: %s", p.ID)
	}
	base := filepath.Base(filepath.Clean(dir))
	if base != p.ID {
		return errors.Errorf("插件目录名必须与插件 ID 一致: dir=%s id=%s", base, p.ID)
	}
	if p.Name == "" {
		return errors.New("插件描述文件(plugin.yaml)中缺少插件名称")
	}
	if p.Assets.Dir != "" && !safeRelativeDir(p.Assets.Dir) {
		return errors.Errorf("插件资源目录不能包含特殊路径: %s", p.Assets.Dir)
	}
	seenWidgets := make(map[string]bool)
	for _, w := range p.Widgets {
		if w.ID == "" {
			return errors.Errorf("插件[%s]存在缺少 ID 的组件声明", p.ID)
		}
		if !pluginIDPattern.MatchString(w.ID) {
			return errors.Errorf("插件组件 ID 只能包含小写字母、数字、下划线和短横线: %s", w.ID)
		}
		if seenWidgets[w.ID] {
			return errors.Errorf("插件[%s]重复声明组件: %s", p.ID, w.ID)
		}
		seenWidgets[w.ID] = true
	}
	return nil
}

func safeRelativeDir(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\x00") {
		return false
	}
	clean := filepath.Clean(path)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// PluginDomain 返回插件翻译使用的文本域名。
func (p *Plugin) PluginDomain() string {
	if p == nil {
		return ""
	}
	return "plugin_" + p.ID
}

// OptionKey 返回插件全局 option 在 Setting 表中的 key。
func OptionKey(pluginID, optionID string) string {
	return fmt.Sprintf("plugin_%s_%s", pluginID, optionID)
}

// GetOption 从 Setting 表读取插件 option 值，未配置时返回 default 值。
func GetOption(getSetting func(key string) (string, error), pluginID string, opt OptionDecl) string {
	key := OptionKey(pluginID, opt.ID)
	val, err := getSetting(key)
	if err != nil || val == "" {
		return opt.Default
	}
	return val
}

// GetOptionByID 从插件选项声明列表中查找指定 option 并读取其配置值。
func GetOptionByID(getSetting func(key string) (string, error), pluginID string, options []OptionDecl, optionID string) string {
	for _, opt := range options {
		if opt.ID == optionID {
			return GetOption(getSetting, pluginID, opt)
		}
	}
	return ""
}

// LoadTranslations 把插件 i18n 目录绑定到插件 ID 对应的文本域。
func (p *Plugin) LoadTranslations() error {
	if p == nil {
		return nil
	}
	return i18n.BindDomain(p.PluginDomain(), p.I18nDir())
}

// FunctionsPath 返回插件 functions 脚本路径。
func (p *Plugin) FunctionsPath() string {
	if p == nil {
		return ""
	}
	for _, name := range []string{"functions.go", "functions.goyaegi"} {
		path := filepath.Join(p.Dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// TemplatesDir 返回插件模板目录路径。
func (p *Plugin) TemplatesDir() string {
	if p == nil {
		return ""
	}
	return filepath.Join(p.Dir, "templates")
}

// AssetsDir 返回插件静态资源目录路径。
func (p *Plugin) AssetsDir() string {
	if p == nil {
		return ""
	}
	dir := p.Assets.Dir
	if dir == "" {
		dir = "assets"
	}
	return filepath.Join(p.Dir, dir)
}

// I18nDir 返回插件自带翻译文件目录路径。
func (p *Plugin) I18nDir() string {
	if p == nil {
		return ""
	}
	return filepath.Join(p.Dir, "i18n")
}
