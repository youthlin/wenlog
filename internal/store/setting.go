// Package store — 设置相关方法。
package store

import (
	"context"
	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/model"
	"gorm.io/gorm"
)

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var st model.Setting
	err := s.db.First(&st, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", errors.Wrap(err, "get setting")
	}
	return st.Value, nil
}
func (s *Store) GetSettings(ctx context.Context, keys ...string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	var items []model.Setting
	if err := s.db.Where("key IN ?", keys).Find(&items).Error; err != nil {
		return nil, errors.Wrap(err, "list settings")
	}
	for _, key := range keys {
		out[key] = ""
	}
	for _, item := range items {
		out[item.Key] = item.Value
	}
	return out, nil
}
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	st := &model.Setting{Key: key, Value: value}
	return errors.Wrap(s.db.Save(st).Error, "save setting")
}
func (s *Store) DebugQuery(ctx context.Context, sql string) ([]map[string]any, error) {
	rows, err := s.db.Raw(sql).Rows()
	if err != nil {
		return nil, errors.Wrap(err, "debug query")
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, errors.Wrap(err, "debug query columns")
	}
	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, errors.Wrap(err, "debug query scan")
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			v := values[i]
			if b, ok := v.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = v
			}
		}
		result = append(result, row)
	}
	return result, nil
}
