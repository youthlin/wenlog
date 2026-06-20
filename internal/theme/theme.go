// Package theme 提供主题管理功能：解析 theme.yaml、管理主题生命周期。
package theme

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
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
	// WidgetAreas 是主题声明的组件区域（v4）。
	WidgetAreas map[string]WidgetArea `yaml:"widget_areas" json:"widget_areas"`
	// Widgets 是主题声明的可用组件列表（v4）。
	Widgets []WidgetDecl `yaml:"widgets" json:"widgets"`
	// Options 是主题声明的可配置选项（v5）。
	Options []OptionDecl `yaml:"options" json:"options"`
}

// WidgetArea 描述一个可配置的组件区域。
type WidgetArea struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

// WidgetDecl 描述主题声明的一个可用组件。
type WidgetDecl struct {
	ID   string `yaml:"id" json:"id"`
	Area string `yaml:"area" json:"area"`
}

// OptionDecl 描述主题声明的一个可配置选项（v5）。
type OptionDecl struct {
	ID          string       `yaml:"id" json:"id"`
	Type        string       `yaml:"type" json:"type"` // text, textarea, number, image, color, css, html, select, bool
	Label       string       `yaml:"label" json:"label"`
	Description string       `yaml:"description" json:"description"`
	Default     string       `yaml:"default" json:"default"`
	Min         *float64     `yaml:"min" json:"min,omitempty"`     // number 类型的最小值
	Max         *float64     `yaml:"max" json:"max,omitempty"`     // number 类型的最大值
	Options     []SelectOpt  `yaml:"options" json:"options,omitempty"` // select 类型的选项
}

// SelectOpt 是 select 类型选项的一个可选项。
type SelectOpt struct {
	Value string `yaml:"value" json:"value"`
	Label string `yaml:"label" json:"label"`
}

// LoadTheme 从目录加载 theme.yaml 并返回 Theme。
func LoadTheme(dir string) (*Theme, error) {
	path := filepath.Join(dir, "theme.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "read theme.yaml from %s", dir)
	}
	var t Theme
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, errors.Wrapf(err, "parse theme.yaml from %s", dir)
	}
	if t.Name == "" {
		return nil, errors.New("theme.yaml: name is required")
	}
	if t.Name == "." || t.Name == ".." || strings.ContainsAny(t.Name, `/\`) {
		return nil, errors.New("theme.yaml: name must not contain path separators")
	}
	t.Dir = dir
	return &t, nil
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

// LoadTranslations 把主题 i18n 目录绑定到主题名称对应的文本域。
func (t *Theme) LoadTranslations() error {
	if t == nil {
		return nil
	}
	return i18n.BindDomain(t.Name, t.I18nDir())
}
