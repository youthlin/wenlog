package theme

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// ThemeDir 返回指定主题的根目录路径。
func (m *Manager) ThemeDir(name string) string {
	t := m.Get(name)
	if t == nil {
		return ""
	}
	return t.Dir
}

// ThemeFilePath 校验并返回主题文件的安全路径。
// 防止路径穿越攻击。
func (m *Manager) ThemeFilePath(name, relPath string) (string, error) {
	t := m.Get(name)
	if t == nil {
		return "", fmt.Errorf("theme %q not found", name)
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("absolute path is not allowed: %s", relPath)
	}
	clean := filepath.Clean(relPath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal detected: %s", relPath)
	}
	base, err := filepath.Abs(t.Dir)
	if err != nil {
		return "", err
	}
	fullPath, err := filepath.Abs(filepath.Join(base, clean))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, fullPath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path traversal detected: %s", relPath)
	}
	return fullPath, nil
}

// ListThemeFiles 列出主题目录下的所有可编辑文件。
func (m *Manager) ListThemeFiles(name string) ([]ThemeFile, error) {
	t := m.Get(name)
	if t == nil {
		return nil, fmt.Errorf("theme %q not found", name)
	}
	var files []ThemeFile
	err := filepath.WalkDir(t.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(t.Dir, path)
		if err != nil {
			return err
		}
		if !isEditableThemeFile(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, ThemeFile{
			Path: rel,
			Size: info.Size(),
		})
		return nil
	})
	return files, err
}

// ThemeFile 表示主题中的一个可编辑文件。
type ThemeFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// isEditableThemeFile 检查文件是否可在线编辑。
func isEditableThemeFile(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".gohtml", ".html", ".css", ".js", ".yaml", ".yml", ".po", ".mo":
		return true
	case ".go", ".goyaegi":
		// 只允许 functions.go / functions.goyaegi
		base := filepath.Base(path)
		return base == "functions.go" || base == "functions.goyaegi"
	default:
		return false
	}
}
