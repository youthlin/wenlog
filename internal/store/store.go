// Package store 负责数据库连接、迁移以及仓储查询。
package store

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/youthlin/wenlog/internal/model"
)

// Store 封装 gorm DB。
type Store struct {
	gormDB *gorm.DB
	dbPath string

	cacheMu sync.RWMutex
	cache   *DataLoader
}

// Open 打开 SQLite 数据库并执行自动迁移。
func Open(dbPath string) (*Store, error) {
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, errors.Wrap(err, "创建数据库目录失败")
		}
	}
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, errors.Wrap(err, "打开数据库文件失败")
	}
	// 注册 SQL 追踪插件，记录每次 SQL 执行的详情（可通过 ctx 注入 SQLDetails 收集）。
	if err := db.Use(&GormSQLTracer{}); err != nil {
		return nil, errors.Wrap(err, "注册SQL语句追踪插件失败")
	}
	s := &Store{gormDB: db, dbPath: dbPath}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// DB 返回带 context 的 gorm 句柄，确保 SQL 追踪插件能拿到 ctx。
func (s *Store) DB(ctx context.Context) *gorm.DB {
	return s.gormDB.WithContext(ctx)
}

func (s *Store) migrate() error {
	err := s.gormDB.AutoMigrate(
		&model.User{},
		&model.PendingRegistration{},
		&model.PendingEmailChange{},
		&model.Post{},
		&model.Category{},
		&model.Tag{},
		&model.Comment{},
		&model.Setting{},
		&model.Upload{},
		&model.PostRevision{},
	)
	if err != nil {
		return errors.Wrap(err, "数据表自动迁移升级失败")
	}
	return nil
}
