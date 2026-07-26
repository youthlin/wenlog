package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/i18n"
	"github.com/youthlin/wenlog/internal/script"
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
	DefaultEnabled bool              `yaml:"default_enabled" json:"default_enabled"`
	Dir            string            `yaml:"-" json:"-"`
	Hooks          HookDecl          `yaml:"hooks" json:"hooks"`
	Lifecycle      LifecycleDecl     `yaml:"lifecycle" json:"lifecycle"`
	Widgets        []WidgetDecl      `yaml:"widgets" json:"widgets"`
	Options        []hook.OptionDecl `yaml:"options" json:"options"`
	Assets         AssetDecl         `yaml:"assets" json:"assets"`
}

// HookDecl 描述插件声明会使用的标准 action/filter。
// 该声明主要用于后台展示和能力说明，实际注册以 functions.goyaegi 为准。
type HookDecl struct {
	Actions []string `yaml:"actions" json:"actions"`
	Filters []string `yaml:"filters" json:"filters"`
}

// LifecycleDecl 描述插件脚本实现的生命周期入口。
// 该声明用于后台展示和能力说明，实际调用仍以 functions.goyaegi 中的函数为准。
type LifecycleDecl struct {
	Activate   bool `yaml:"activate" json:"activate"`
	Deactivate bool `yaml:"deactivate" json:"deactivate"`
	Uninstall  bool `yaml:"uninstall" json:"uninstall"`
}

// WidgetDecl 是 hook.WidgetDecl 的别名，描述插件提供的一类组件。
type WidgetDecl = hook.WidgetDecl

// 以下类型别名指向 hook 包，避免各包重复定义。
type OptionDecl = hook.OptionDecl
type SelectOpt = hook.SelectOpt

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
		p.Widgets[i].Source = "plugin"
		p.Widgets[i].PluginID = p.ID
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
	return script.FindFunctionsPath(p.Dir)
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
