package store

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// backupDir 返回备份目录路径（相对于 db 文件所在目录）。
func (s *Store) backupDir() string {
	return filepath.Join(filepath.Dir(s.dbPath), "backups")
}

// DBPath 返回数据库文件路径。
func (s *Store) DBPath() string { return s.dbPath }

// BackupDB 执行 WAL checkpoint 后将数据库文件复制到备份目录。
// 返回备份文件路径。
func (s *Store) BackupDB() (string, error) {
	// WAL checkpoint：将 WAL 内容合并回主数据库文件
	sqlDB, err := s.gormDB.DB()
	if err != nil {
		return "", errors.Wrap(err, "get sql.DB")
	}
	if _, err := sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return "", errors.Wrap(err, "wal checkpoint")
	}

	dir := s.backupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.Wrap(err, "create backup dir")
	}

	now := time.Now()
	name := fmt.Sprintf("wenlog-%s.db", now.Format("20060102-150405"))
	dst := filepath.Join(dir, name)

	if err := copyFile(s.dbPath, dst); err != nil {
		return "", errors.Wrap(err, "copy db file")
	}

	return dst, nil
}

// RestoreDB 从备份文件恢复数据库。
// 恢复前会验证备份文件是否为有效 SQLite 数据库，
// 并自动创建紧急备份以防误操作。
func (s *Store) RestoreDB(backupPath string) error {
	// 1. 验证备份文件是否为有效 SQLite 数据库
	if err := validateSQLiteFile(backupPath); err != nil {
		return errors.Wrap(err, "invalid backup file")
	}

	// 2. 创建紧急备份（恢复前的当前数据库）
	emergencyName := fmt.Sprintf("wenlog-emergency-%s.db", time.Now().Format("20060102-150405"))
	emergencyPath := filepath.Join(s.backupDir(), emergencyName)
	if err := os.MkdirAll(s.backupDir(), 0o755); err != nil {
		return errors.Wrap(err, "create backup dir for emergency")
	}
	if err := copyFile(s.dbPath, emergencyPath); err != nil {
		return errors.Wrap(err, "create emergency backup")
	}

	// 3. 关闭当前数据库连接
	sqlDB, err := s.gormDB.DB()
	if err != nil {
		return errors.Wrap(err, "get sql.DB")
	}
	if err := sqlDB.Close(); err != nil {
		return errors.Wrap(err, "close db")
	}

	// 4. 复制备份文件到数据库路径
	if err := copyFile(backupPath, s.dbPath); err != nil {
		// 复制失败，尝试恢复紧急备份
		_ = copyFile(emergencyPath, s.dbPath)
		// 尝试重新打开数据库
		s.reopenDB()
		return errors.Wrap(err, "copy backup to db path, emergency backup restored")
	}

	// 5. 重新打开数据库
	if err := s.reopenDB(); err != nil {
		// 重新打开失败，尝试恢复紧急备份
		_ = copyFile(emergencyPath, s.dbPath)
		_ = s.reopenDB()
		return errors.Wrap(err, "reopen db after restore, emergency backup restored")
	}

	// 6. 刷新缓存
	s.InvalidateCache()

	return nil
}

// reopenDB 重新打开数据库连接。
func (s *Store) reopenDB() error {
	db, err := gorm.Open(sqlite.Open(s.dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return errors.Wrap(err, "open sqlite")
	}
	if err := db.Use(&GormSQLTracer{}); err != nil {
		return errors.Wrap(err, "register sql tracer")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return errors.Wrap(err, "get sql.DB")
	}
	// 关闭旧的 sql.DB（如果还存在）
	if oldDB, _ := s.gormDB.DB(); oldDB != nil {
		_ = oldDB.Close()
	}
	s.gormDB = db
	_ = sqlDB // 保持连接
	return nil
}

// BackupInfo 备份文件信息。
type BackupInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// ListBackups 列出所有备份文件，按时间倒序。
func (s *Store) ListBackups() ([]BackupInfo, error) {
	dir := s.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "read backup dir")
	}

	var backups []BackupInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, BackupInfo{
			Name:    entry.Name(),
			Path:    filepath.Join(dir, entry.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].ModTime.After(backups[j].ModTime)
	})

	return backups, nil
}

// DeleteBackup 删除指定备份文件。
func (s *Store) DeleteBackup(filename string) error {
	// 安全检查：只允许删除 .db 文件，防止路径穿越
	name := filepath.Base(filename)
	if !strings.HasSuffix(name, ".db") {
		return fmt.Errorf("invalid backup filename: %s", filename)
	}
	path := filepath.Join(s.backupDir(), name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "delete backup")
	}
	return nil
}

// CleanOldBackups 保留最近 keep 个备份，删除更早的。
func (s *Store) CleanOldBackups(keep int) error {
	backups, err := s.ListBackups()
	if err != nil {
		return err
	}
	if len(backups) <= keep {
		return nil
	}
	for _, b := range backups[keep:] {
		_ = os.Remove(b.Path)
	}
	return nil
}

// validateSQLiteFile 验证文件是否为有效 SQLite 数据库（检查文件头魔数）。
func validateSQLiteFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.Wrap(err, "open file")
	}
	defer f.Close()

	header := make([]byte, 16)
	n, err := f.Read(header)
	if err != nil {
		return errors.Wrap(err, "read header")
	}
	if n < 16 {
		return fmt.Errorf("file too small: %d bytes", n)
	}

	// SQLite 数据库文件头: "SQLite format 3\000"
	magic := string(header)
	if magic != "SQLite format 3\x00" {
		return fmt.Errorf("not a valid SQLite database file")
	}
	return nil
}

// copyFile 复制文件。
func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	return d.Sync()
}

// 确保 sql 包被使用（reopenDB 中用到但未直接引用类型）
var _ = (*sql.DB)(nil)
