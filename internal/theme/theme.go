// Package theme 提供主题管理功能：解析 theme.yaml、管理主题生命周期。
package theme

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/i18n"
	"gopkg.in/yaml.v3"
)

// Theme 表示一个已安装的主题。
type Theme struct {
	Name        string                `yaml:"name" json:"name"`
	Version     string                `yaml:"version" json:"version"`
	Description string                `yaml:"description" json:"description"`
	Author      string                `yaml:"author" json:"author"`
	Pages       map[string]PageConfig `yaml:"pages" json:"pages"`
	// Dir 是主题在磁盘上的根目录（如 themes/default），由 Manager 填充。
	Dir string `yaml:"-" json:"-"`
}

// PageConfig 描述某个页面的配置（v3 中退化为空，保留以兼容旧 theme.yaml）。
type PageConfig struct {
	// Data 和 Widgets 在 v3 中不再使用，保留字段以兼容旧配置。
	Data    []string `yaml:"data" json:"data"`
	Widgets []string `yaml:"widgets" json:"widgets"`
}

// Needs 检查页面配置是否声明了某个数据字段（v3 中始终返回 true，数据全局可用）。
func (pc PageConfig) Needs(field string) bool {
	return true
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
	// 确保 Pages 不为 nil
	if t.Pages == nil {
		t.Pages = make(map[string]PageConfig)
	}
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

// LoadTranslations 把主题 i18n 目录绑定到主题名称对应的文本域。
func (t *Theme) LoadTranslations() error {
	if t == nil {
		return nil
	}
	return i18n.BindDomain(t.Name, t.I18nDir())
}
