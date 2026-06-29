// Package theme 提供主题管理功能：解析 theme.yaml、管理主题生命周期。
package theme

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/i18n"
	"gopkg.in/yaml.v3"
)

// Theme 表示一个已安装的主题。
type Theme struct {
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description" json:"description"`
	Author      string `yaml:"author" json:"author"`
	// Screenshot 是主题目录内的截图文件名（如 screenshot.png），为空表示无截图。
	Screenshot string `yaml:"screenshot" json:"screenshot"`
	// ThemeURI 是主题主页 URL。
	ThemeURI string `yaml:"theme_uri" json:"theme_uri"`
	// AuthorURI 是作者主页 URL。
	AuthorURI string `yaml:"author_uri" json:"author_uri"`
	// License 是许可证名称（如 MIT、GPL-2.0）。
	License string `yaml:"license" json:"license"`
	// LicenseURI 是许可证全文 URL。
	LicenseURI string `yaml:"license_uri" json:"license_uri"`
	// Tags 是主题标签列表。
	Tags []string `yaml:"tags" json:"tags"`
	// Dir 是主题在磁盘上的根目录（如 themes/default），由 Manager 填充。
	Dir string `yaml:"-" json:"-"`
	// WidgetAreas 是主题声明的组件区域。
	WidgetAreas map[string]WidgetArea `yaml:"widget_areas" json:"widget_areas"`
	// MenuLocations 是主题声明的菜单位置，如 primary/footer。
	MenuLocations map[string]MenuLocation `yaml:"menu_locations" json:"menu_locations"`
	// Widgets 是主题声明的可用组件列表。
	Widgets []WidgetDecl `yaml:"widgets" json:"widgets"`
	// Options 是主题声明的全局可配置选项。
	Options []hook.OptionDecl `yaml:"options" json:"options"`
	// WidgetTemplates 是主题 widgets/ 目录中实际存在的组件模板 ID 集合。
	WidgetTemplates map[string]bool `yaml:"-" json:"-"`
}

// WidgetArea 描述一个可配置的组件区域。
type WidgetArea struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

// MenuLocation 描述一个主题菜单位置。
type MenuLocation struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

// WidgetDecl 是 hook.WidgetDecl 的别名，描述主题声明的一个可用组件。
type WidgetDecl = hook.WidgetDecl

// 以下类型别名指向 hook 包，避免各包重复定义。
type OptionDecl = hook.OptionDecl
type SelectOpt = hook.SelectOpt

// LoadTheme 从目录加载 theme.yaml 并返回 Theme。
func LoadTheme(dir string) (*Theme, error) {
	path := filepath.Join(dir, "theme.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "读取[%s]中的主题描述文件(theme.yaml)失败", dir)
	}
	var t Theme
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, errors.Wrapf(err, "解析[%s]中的主题描述文件(theme.yaml)失败", dir)
	}
	if t.Name == "" {
		return nil, errors.New("主题描述文件(theme.yaml)中缺少主题名称")
	}
	if t.Name == "." || t.Name == ".." || strings.ContainsAny(t.Name, `/\`) {
		return nil, errors.Errorf("主题名称不能含有特殊字符: %s", t.Name)
	}
	t.Dir = dir
	t.WidgetTemplates = loadWidgetTemplates(t.WidgetsDir())
	return &t, nil
}

func loadWidgetTemplates(dir string) map[string]bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	ids := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".gohtml" {
			continue
		}
		ids[strings.TrimSuffix(entry.Name(), ".gohtml")] = true
	}
	return ids
}

// ThemeDomain 返回主题翻译使用的文本域名（加 theme_ 前缀避免与应用默认域冲突）。
func (t *Theme) ThemeDomain() string {
	if t == nil {
		return ""
	}
	return "theme_" + t.Name
}

// LoadTranslations 把主题 i18n 目录绑定到主题名称对应的文本域。
func (t *Theme) LoadTranslations() error {
	if t == nil {
		return nil
	}
	return i18n.BindDomain(t.ThemeDomain(), t.I18nDir())
}

// TemplatesDir 返回主题的模板目录路径。
func (t *Theme) TemplatesDir() string {
	return filepath.Join(t.Dir, "templates")
}

// AssetsDir 返回主题的静态资源目录路径。
func (t *Theme) AssetsDir() string {
	return filepath.Join(t.Dir, "assets")
}

// I18nDir 返回主题自带翻译文件目录路径。
func (t *Theme) I18nDir() string {
	return filepath.Join(t.Dir, "i18n")
}

// WidgetsDir 返回主题的组件模板目录路径。
func (t *Theme) WidgetsDir() string {
	return filepath.Join(t.Dir, "widgets")
}

// HasTemplates 检查主题是否包含模板目录。
func (t *Theme) HasTemplates() bool {
	info, err := os.Stat(t.TemplatesDir())
	if err != nil || !info.IsDir() {
		return false
	}
	matches, err := filepath.Glob(filepath.Join(t.TemplatesDir(), "*.gohtml"))
	return err == nil && len(matches) > 0
}

// HasAssets 检查主题是否包含静态资源目录。
func (t *Theme) HasAssets() bool {
	info, err := os.Stat(t.AssetsDir())
	return err == nil && info.IsDir()
}

// ScreenshotURL 返回主题截图的访问 URL，无截图时返回空字符串。
func (t *Theme) ScreenshotURL() string {
	if t.Screenshot == "" {
		return ""
	}
	// 安全检查：只允许文件名，不允许路径
	name := filepath.Base(t.Screenshot)
	if name == "." || name == ".." || name != t.Screenshot {
		return ""
	}
	// 检查文件是否存在
	fullPath := filepath.Join(t.Dir, name)
	if _, err := os.Stat(fullPath); err != nil {
		return ""
	}
	return "/admin/theme/screenshot/" + url.PathEscape(t.Name) + "/" + url.PathEscape(name)
}
