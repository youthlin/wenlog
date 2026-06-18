// Package theme 提供主题管理功能：解析 theme.yaml、管理主题生命周期。
package theme

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
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

// PageConfig 描述某个页面需要的数据和 sidebar widget。
type PageConfig struct {
	Data    []string `yaml:"data" json:"data"`
	Sidebar []string `yaml:"sidebar" json:"sidebar"`
}

// Needs 检查页面配置是否声明了某个数据字段。
func (pc PageConfig) Needs(field string) bool {
	for _, d := range pc.Data {
		if d == field {
			return true
		}
	}
	return false
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

// HasTemplates 检查主题是否包含模板目录。
func (t *Theme) HasTemplates() bool {
	info, err := os.Stat(t.TemplatesDir())
	return err == nil && info.IsDir()
}

// HasAssets 检查主题是否包含静态资源目录。
func (t *Theme) HasAssets() bool {
	info, err := os.Stat(t.AssetsDir())
	return err == nil && info.IsDir()
}
