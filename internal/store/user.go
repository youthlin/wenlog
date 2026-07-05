// Package store — 用户相关方法。
package store

import (
	"context"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/wenlog/internal/model"
	"gorm.io/gorm"
)

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	if err := s.DB(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Store) GetUserByID(ctx context.Context, id uint) (*model.User, error) {
	var u model.User
	if err := s.DB(ctx).First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User
	if err := s.DB(ctx).Where("email = ?", email).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Store) SetResetToken(ctx context.Context, userID uint, token string, expiry time.Time) error {
	return errors.Wrap(
		s.DB(ctx).Model(&model.User{}).Where("id = ?", userID).
			Updates(map[string]any{"reset_token": token, "reset_token_expiry": expiry}).Error,
		"set reset token")
}
func (s *Store) GetUserByResetToken(ctx context.Context, token string) (*model.User, error) {
	var u model.User
	err := s.DB(ctx).Where("reset_token = ? AND reset_token_expiry > ?", token, time.Now()).First(&u).Error
	if err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Store) ClearResetToken(ctx context.Context, userID uint) error {
	return errors.Wrap(
		s.DB(ctx).Model(&model.User{}).Where("id = ?", userID).
			Updates(map[string]any{"reset_token": "", "reset_token_expiry": time.Time{}}).Error,
		"clear reset token")
}
func (s *Store) UserExistsByUsername(ctx context.Context, username string, excludeID uint) (bool, error) {
	var n int64
	q := s.DB(ctx).Model(&model.User{}).Where("username = ?", username)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count user by username")
}
func (s *Store) UserExistsByEmail(ctx context.Context, email string, excludeID uint) (bool, error) {
	var n int64
	q := s.DB(ctx).Model(&model.User{}).Where("email = ?", email)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count user by email")
}
func (s *Store) SavePendingRegistration(ctx context.Context, username, email, token string, expiry time.Time) error {
	return errors.Wrap(s.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("username = ? OR email = ?", username, email).Delete(&model.PendingRegistration{}).Error; err != nil {
			return err
		}
		return tx.Create(&model.PendingRegistration{
			Username:    username,
			Email:       email,
			Token:       token,
			TokenExpiry: expiry,
		}).Error
	}), "save pending registration")
}
func (s *Store) GetPendingRegistrationByToken(ctx context.Context, token string) (*model.PendingRegistration, error) {
	var pr model.PendingRegistration
	err := s.DB(ctx).Where("token = ? AND token_expiry > ?", token, time.Now()).First(&pr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPendingRegistrationNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "get pending registration")
	}
	return &pr, nil
}
func (s *Store) CompletePendingRegistration(ctx context.Context, token, passwordHash string) (*model.User, error) {
	var out model.User
	err := s.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var pr model.PendingRegistration
		if err := tx.Where("token = ? AND token_expiry > ?", token, time.Now()).First(&pr).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPendingRegistrationNotFound
			}
			return err
		}
		var count int64
		if err := tx.Model(&model.User{}).Where("username = ? OR email = ?", pr.Username, pr.Email).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrPendingRegistrationNotFound
		}
		out = model.User{
			Username:     pr.Username,
			DisplayName:  pr.Username,
			Email:        pr.Email,
			PasswordHash: passwordHash,
			Role:         model.RoleSubscriber,
		}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		return tx.Delete(&model.PendingRegistration{}, pr.ID).Error
	})
	return &out, errors.Wrap(err, "complete pending registration")
}
func (s *Store) SavePendingEmailChange(ctx context.Context, userID uint, email, token string, expiry time.Time) error {
	return errors.Wrap(s.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? OR email = ?", userID, email).Delete(&model.PendingEmailChange{}).Error; err != nil {
			return err
		}
		return tx.Create(&model.PendingEmailChange{
			UserID:      userID,
			Email:       email,
			Token:       token,
			TokenExpiry: expiry,
		}).Error
	}), "save pending email change")
}
func (s *Store) CompletePendingEmailChange(ctx context.Context, userID uint, token string) (*model.User, error) {
	var out model.User
	err := s.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var pending model.PendingEmailChange
		if err := tx.Where("user_id = ? AND token = ? AND token_expiry > ?", userID, token, time.Now()).First(&pending).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPendingEmailChangeNotFound
			}
			return err
		}
		var count int64
		if err := tx.Model(&model.User{}).Where("email = ? AND id <> ?", pending.Email, userID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrPendingEmailChangeNotFound
		}
		if err := tx.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{"email": pending.Email, "session_version": gorm.Expr("session_version + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&model.PendingEmailChange{}, pending.ID).Error; err != nil {
			return err
		}
		return tx.First(&out, userID).Error
	})
	return &out, errors.Wrap(err, "complete pending email change")
}
func (s *Store) ListUsers(ctx context.Context) ([]model.User, error) {
	var users []model.User
	err := s.DB(ctx).Order("display_name ASC").Order("username ASC").Find(&users).Error
	return users, errors.Wrap(err, "list users")
}
func (s *Store) ListUsersByRole(ctx context.Context, role string) ([]model.User, error) {
	var users []model.User
	err := s.DB(ctx).Where("role = ?", role).Order("display_name ASC").Order("username ASC").Find(&users).Error
	return users, errors.Wrapf(err, "list users by role=%s", role)
}
func (s *Store) CountUsers(ctx context.Context) (count int64, err error) {
	err = s.DB(ctx).Model(&model.User{}).Count(&count).Error
	err = errors.Wrapf(err, "查询用户数量失败")
	return
}

func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string) error {
	var u model.User
	err := s.DB(ctx).Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		u = model.User{
			Username:     username,
			DisplayName:  username,
			PasswordHash: passwordHash,
			Role:         model.RoleAdmin,
		}
		err = s.DB(ctx).Create(&u).Error
		return errors.Wrapf(err, "创建管理员失败, username=%s", username)
	}
	if err != nil {
		return errors.Wrapf(err, "查询管理员失败, username=%s", username)
	}
	return errors.Errorf("创建管理员失败, 已存在管理员: %s(%s)", u.Username, u.DisplayName)
}

func (s *Store) SetUserPassword(ctx context.Context, username, passwordHash string) error {
	result := s.DB(ctx).Model(&model.User{}).Where("username = ?", username).
		Updates(map[string]any{"password_hash": passwordHash, "session_version": gorm.Expr("session_version + 1")})
	if result.Error != nil {
		return errors.Wrapf(result.Error, "set user password, username=%s", username)
	}
	if result.RowsAffected == 0 {
		return errors.Newf("用户不存在: %s", username)
	}
	return nil
}
func (s *Store) CreateUser(ctx context.Context, username, displayName, email, passwordHash, role string) error {
	u := model.User{
		Username:     username,
		DisplayName:  displayName,
		Email:        email,
		PasswordHash: passwordHash,
		Role:         role,
	}
	return errors.Wrapf(s.DB(ctx).Create(&u).Error, "create user, username=%s", username)
}
func (s *Store) UpdateUserProfile(ctx context.Context, id uint, username, displayName, email, website string) error {
	return errors.Wrap(s.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var u model.User
		if err := tx.Select("id", "email").First(&u, id).Error; err != nil {
			return err
		}
		updates := map[string]any{"username": username, "display_name": displayName, "email": email, "website": website}
		if !strings.EqualFold(strings.TrimSpace(u.Email), strings.TrimSpace(email)) {
			updates["session_version"] = gorm.Expr("session_version + 1")
		}
		return tx.Model(&model.User{}).Where("id = ?", id).Updates(updates).Error
	}), "update user profile")
}
func (s *Store) TouchUserSessionVersion(ctx context.Context, id uint) error {
	return errors.Wrap(
		s.DB(ctx).Model(&model.User{}).Where("id = ?", id).Update("session_version", gorm.Expr("session_version + 1")).Error,
		"touch user session version")
}
func (s *Store) UpdateUserPassword(ctx context.Context, id uint, passwordHash string) error {
	return errors.Wrap(
		s.DB(ctx).Model(&model.User{}).Where("id = ?", id).
			Updates(map[string]any{"password_hash": passwordHash, "session_version": gorm.Expr("session_version + 1")}).Error,
		"update user password")
}
func (s *Store) AdminListUsers(ctx context.Context, page, pageSize int) ([]model.User, int64, error) {
	q := s.DB(ctx).Model(&model.User{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count users")
	}
	var users []model.User
	err := q.Order("created_at ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error
	return users, total, errors.Wrap(err, "admin list users")
}
func (s *Store) UpdateUserRole(ctx context.Context, id uint, role string) error {
	return s.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var u model.User
		if err := tx.First(&u, id).Error; err != nil {
			return errors.Wrap(err, "load user before role update")
		}
		if u.Role == model.RoleAdmin && role != model.RoleAdmin {
			adminCount, err := countAdmins(tx)
			if err != nil {
				return err
			}
			if adminCount <= 1 {
				return ErrLastAdmin
			}
		}
		return errors.Wrap(
			tx.Model(&model.User{}).Where("id = ?", id).Updates(map[string]any{"role": role, "session_version": gorm.Expr("session_version + 1")}).Error,
			"update user role")
	})
}
func (s *Store) DeleteUser(ctx context.Context, id uint) error {
	return s.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var u model.User
		if err := tx.First(&u, id).Error; err != nil {
			return errors.Wrap(err, "load user before delete")
		}
		if u.Role == model.RoleAdmin {
			adminCount, err := countAdmins(tx)
			if err != nil {
				return err
			}
			if adminCount <= 1 {
				return ErrLastAdmin
			}
		}
		// 将该用户的评论设为匿名(清除 UserID)
		if err := tx.Model(&model.Comment{}).Where("user_id = ?", id).
			Update("user_id", nil).Error; err != nil {
			return errors.Wrap(err, "anonymize user comments")
		}
		if err := tx.Delete(&model.User{}, id).Error; err != nil {
			return errors.Wrap(err, "delete user")
		}
		return nil
	})
}
func (s *Store) EnsureAdminRole(ctx context.Context, username string) error {
	return errors.Wrap(
		s.DB(ctx).Model(&model.User{}).Where("username = ?", username).
			Update("role", model.RoleAdmin).Error,
		"ensure admin role")
}
func (s *Store) ExportUserData(ctx context.Context, userID uint) (*UserExportData, error) {
	u, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, errors.Wrap(err, "get user for export")
	}
	eu := ExportUser{
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Website:     u.Website,
		Role:        u.Role,
		CreatedAt:   u.CreatedAt,
	}
	var comments []model.Comment
	if err := s.DB(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&comments).Error; err != nil {
		return nil, errors.Wrap(err, "list user comments for export")
	}
	ec := make([]ExportComment, 0, len(comments))
	for _, c := range comments {
		ec = append(ec, ExportComment{
			ID:        c.ID,
			PostID:    c.PostID,
			Author:    c.Author,
			Email:     c.Email,
			URL:       c.URL,
			Content:   c.Content,
			Status:    c.Status,
			CreatedAt: c.CreatedAt,
		})
	}
	return &UserExportData{User: eu, Comments: ec}, nil
}
