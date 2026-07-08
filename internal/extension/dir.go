package extension

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
)

// SafeRelativePath 判断路径是否是安全的相对路径。
func SafeRelativePath(name string) bool {
	if name == "." || name == ".." || filepath.IsAbs(name) || strings.Contains(name, "\x00") {
		return false
	}
	return !strings.HasPrefix(name, ".."+string(filepath.Separator))
}

// PathWithinDir 判断 target 解析后是否仍位于 dir 内。
func PathWithinDir(dir, target string) bool {
	base, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	full, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, full)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// BackupDir 把已存在的目录移动到同级临时备份目录。目录不存在时返回空路径。
func BackupDir(targetDir, label string) (string, error) {
	if _, err := os.Stat(targetDir); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", errors.Wrapf(err, "stat existing %s dir", label)
	}
	backupDir, err := os.MkdirTemp(filepath.Dir(targetDir), ".backup-"+filepath.Base(targetDir)+"-*")
	if err != nil {
		return "", errors.Wrapf(err, "create %s backup dir", label)
	}
	if err := os.Remove(backupDir); err != nil {
		return "", errors.Wrapf(err, "prepare %s backup dir", label)
	}
	if err := os.Rename(targetDir, backupDir); err != nil {
		return "", errors.Wrapf(err, "backup existing %s dir", label)
	}
	return backupDir, nil
}

// RollbackReplace 删除目标目录，并把备份目录移回目标路径。
func RollbackReplace(targetDir, backupDir string) error {
	_ = os.RemoveAll(targetDir)
	if backupDir == "" {
		return nil
	}
	return os.Rename(backupDir, targetDir)
}

// CopyDir 递归复制目录到目标目录。目标目录可以不存在。
func CopyDir(src, dst, label string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := CopyDir(srcPath, dstPath, label); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.Errorf("unsupported %s file type: %s", label, srcPath)
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}
