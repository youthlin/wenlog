// Package store — 后台管理所需的写入/查询方法。
package store

import (
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"gorm.io/gorm"

	"github.com/youthlin/blog/internal/model"
)

func termQueryLike(keyword string) string {
	return "%" + strings.ToLower(strings.TrimSpace(keyword)) + "%"
}

// GetUserByUsername 按用户名查用户(登录用)。
func (s *Store) GetUserByUsername(username string) (*model.User, error) {
	var u model.User
	if err := s.db.Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByID 按 ID 查用户。
func (s *Store) GetUserByID(id uint) (*model.User, error) {
	var u model.User
	if err := s.db.First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// UserExistsByUsername 检查用户名是否已被其他用户占用。
func (s *Store) UserExistsByUsername(username string, excludeID uint) (bool, error) {
	var n int64
	q := s.db.Model(&model.User{}).Where("username = ?", username)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count user by username")
}

// ListUsers 返回全部用户(按显示名/用户名排序),供后台导入等场景选择。
func (s *Store) ListUsers() ([]model.User, error) {
	var users []model.User
	err := s.db.Order("display_name ASC").Order("username ASC").Find(&users).Error
	return users, errors.Wrap(err, "list users")
}

// CountUsers 返回用户总数(首次启动判断是否需建管理员)。
func (s *Store) CountUsers() (count int64, err error) {
	err = s.db.Model(&model.User{}).Count(&count).Error
	err = errors.Wrapf(err, "查询用户数量失败")
	return
}

// UpsertUserPassword 设置/更新用户密码(按 username)。
func (s *Store) UpsertUserPassword(username, displayName, passwordHash string) error {
	var u model.User
	err := s.db.Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		u = model.User{Username: username, DisplayName: displayName, PasswordHash: passwordHash}
		err = s.db.Create(&u).Error
		return errors.Wrapf(err, "创建用户失败, username=%s", username)
	}
	if err != nil {
		return errors.Wrapf(err, "查询用户失败, username=%s", username)
	}
	u.PasswordHash = passwordHash
	if displayName != "" {
		u.DisplayName = displayName
	}
	err = s.db.Save(&u).Error
	return errors.Wrapf(err, "更改密码失败, username=%s", username)
}

// UpdateUserProfile 更新用户用户名、显示名与邮箱。
func (s *Store) UpdateUserProfile(id uint, username, displayName, email string) error {
	return errors.Wrap(
		s.db.Model(&model.User{}).Where("id = ?", id).
			Updates(map[string]any{"username": username, "display_name": displayName, "email": email}).Error,
		"update user profile")
}

// PageSlugExists 检查页面 slug 是否已被其他页面占用。
func (s *Store) PageSlugExists(slug string, excludeID uint) (bool, error) {
	var n int64
	q := s.db.Model(&model.Post{}).Where("post_type = ? AND slug = ?", model.PostTypePage, slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count page slug")
}

// PostSlugExists 检查文章 slug 是否已被其他文章占用。
func (s *Store) PostSlugExists(slug string, excludeID uint) (bool, error) {
	var n int64
	q := s.db.Model(&model.Post{}).Where("post_type = ? AND slug = ?", model.PostTypePost, slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count post slug")
}

// UpdateUserPassword 按用户 ID 更新密码哈希。
func (s *Store) UpdateUserPassword(id uint, passwordHash string) error {
	return errors.Wrap(
		s.db.Model(&model.User{}).Where("id = ?", id).
			Update("password_hash", passwordHash).Error,
		"update user password")
}

// GetSetting 返回指定 key 的设置值,不存在返回空字符串。
func (s *Store) GetSetting(key string) (string, error) {
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

// GetSettings 批量返回若干设置项。
func (s *Store) GetSettings(keys ...string) (map[string]string, error) {
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

// SetSetting 创建或更新一个站点设置。
func (s *Store) SetSetting(key, value string) error {
	st := &model.Setting{Key: key, Value: value}
	return errors.Wrap(s.db.Save(st).Error, "save setting")
}

// DebugQuery 执行只读 SQL 并把结果集转换成 JSON 友好的 map 切片。
func (s *Store) DebugQuery(sql string) ([]map[string]any, error) {
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

// --- 文章/页面管理 ---

// CountPosts 返回文章/页面总数,用于首次启动初始化内容判断。
func (s *Store) CountPosts() (int64, error) {
	var total int64
	err := s.db.Model(&model.Post{}).Count(&total).Error
	return total, errors.Wrap(err, "count posts")
}

// AdminListPosts 返回某类型的全部文章/页面(不限状态),用于后台列表。
func (s *Store) AdminListPosts(postType string, page, pageSize int, categoryID, tagID uint, keyword string) ([]model.Post, int64, error) {
	q := s.db.Model(&model.Post{}).Where("post_type = ?", postType)
	if categoryID > 0 {
		q = q.Joins("JOIN post_categories pc_admin ON pc_admin.post_id = posts.id").Where("pc_admin.category_id = ?", categoryID)
	}
	if tagID > 0 {
		q = q.Joins("JOIN post_tags pt_admin ON pt_admin.post_id = posts.id").Where("pt_admin.tag_id = ?", tagID)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := termQueryLike(kw)
		q = q.Where("LOWER(title) LIKE ? OR LOWER(slug) LIKE ? OR LOWER(content_md) LIKE ? OR LOWER(content) LIKE ?", like, like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count")
	}
	var posts []model.Post
	commentCount := s.db.Model(&model.Comment{}).
		Select("COUNT(1)").
		Where("comments.post_id = posts.id")
	err := q.Select("posts.*, (?) AS comment_count", commentCount).
		Preload("Categories").Preload("Tags").Preload("Author").
		Order("published_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&posts).Error
	return posts, total, errors.Wrap(err, "admin list posts")
}

// AdminGetPost 按 ID 取文章(任意状态,含分类标签)。
func (s *Store) AdminGetPost(id uint) (*model.Post, error) {
	var p model.Post
	err := s.db.Preload("Categories").Preload("Tags").First(&p, id).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// NextPostID 返回下一个可用文章 ID(max+1),避免与历史永久链接冲突。
func (s *Store) NextPostID() (uint, error) {
	var maxID uint
	row := s.db.Model(&model.Post{}).Select("COALESCE(MAX(id),0)").Row()
	if err := row.Scan(&maxID); err != nil {
		return 0, errors.Wrap(err, "max post id")
	}
	return maxID + 1, nil
}

// SavePost 创建或更新文章/页面。
func (s *Store) SavePost(p *model.Post) error {
	return errors.Wrap(s.db.Save(p).Error, "save post")
}

// SavePostWithTerms 保存文章并替换其分类/标签关联(标签按名 upsert)。
// catIDs 为选中的分类 ID;tagNames 为标签名(去空白、去重),不存在则新建。
func (s *Store) SavePostWithTerms(p *model.Post, catIDs []uint, tagNames []string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(p).Error; err != nil {
			return errors.Wrap(err, "save post")
		}
		// 分类:按 ID 取。
		var cats []model.Category
		if len(catIDs) > 0 {
			if err := tx.Where("id IN ?", catIDs).Find(&cats).Error; err != nil {
				return errors.Wrap(err, "load categories")
			}
		}
		if err := tx.Model(p).Association("Categories").Replace(cats); err != nil {
			return errors.Wrap(err, "replace categories")
		}
		// 标签:按 slug(唯一键)查找或新建。
		var tags []model.Tag
		for _, name := range tagNames {
			slug := slugifyTag(name)
			var t model.Tag
			err := tx.Where("slug = ?", slug).First(&t).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				t = model.Tag{Name: name, Slug: slug}
				if err := tx.Create(&t).Error; err != nil {
					return errors.Wrap(err, "create tag")
				}
			} else if err != nil {
				return errors.Wrap(err, "find tag")
			}
			tags = append(tags, t)
		}
		if err := tx.Model(p).Association("Tags").Replace(tags); err != nil {
			return errors.Wrap(err, "replace tags")
		}
		return nil
	})
}

// slugifyTag 由标签名生成 slug:小写、空白转连字符、保留中文与字母数字。
func slugifyTag(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == ' ' || r == '\t':
			b.WriteByte('-')
		case r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127:
			b.WriteRune(r)
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = name
	}
	return slug
}

// DeletePost 删除文章/页面及其关联。
func (s *Store) DeletePost(id uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("post_id = ?", id).Delete(&model.Comment{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM post_categories WHERE post_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM post_tags WHERE post_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Post{}, id).Error
	})
}

// CategorySlugExists 检查分类 slug 是否已被其他分类占用。
func (s *Store) CategorySlugExists(slug string, excludeID uint) (bool, error) {
	var n int64
	q := s.db.Model(&model.Category{}).Where("slug = ?", slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count category slug")
}

// TagSlugExists 检查标签 slug 是否已被其他标签占用。
func (s *Store) TagSlugExists(slug string, excludeID uint) (bool, error) {
	var n int64
	q := s.db.Model(&model.Tag{}).Where("slug = ?", slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, errors.Wrap(err, "count tag slug")
}

// SaveCategory 创建或更新分类。
func (s *Store) SaveCategory(cat *model.Category) error {
	return errors.Wrap(s.db.Save(cat).Error, "save category")
}

// SaveTag 创建或更新标签。
func (s *Store) SaveTag(tag *model.Tag) error {
	return errors.Wrap(s.db.Save(tag).Error, "save tag")
}

// DeleteCategory 删除分类并清理关联与子分类父级。
func (s *Store) DeleteCategory(id uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Category{}).Where("parent_id = ?", id).Update("parent_id", 0).Error; err != nil {
			return errors.Wrap(err, "detach child categories")
		}
		if err := tx.Exec("DELETE FROM post_categories WHERE category_id = ?", id).Error; err != nil {
			return errors.Wrap(err, "delete category relations")
		}
		if err := tx.Delete(&model.Category{}, id).Error; err != nil {
			return errors.Wrap(err, "delete category")
		}
		return nil
	})
}

// DeleteTag 删除标签并清理文章关联。
func (s *Store) DeleteTag(id uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM post_tags WHERE tag_id = ?", id).Error; err != nil {
			return errors.Wrap(err, "delete tag relations")
		}
		if err := tx.Delete(&model.Tag{}, id).Error; err != nil {
			return errors.Wrap(err, "delete tag")
		}
		return nil
	})
}

// --- 评论管理 ---

// AdminListComments 按状态返回评论(分页)。status 为空返回全部。
func (s *Store) AdminListComments(status string, page, pageSize int) ([]model.Comment, int64, error) {
	q := s.db.Model(&model.Comment{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count comments")
	}
	var comments []model.Comment
	err := q.Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&comments).Error
	return comments, total, errors.Wrap(err, "admin list comments")
}

// AdminListCategories 返回后台分类列表(支持搜索与分页)。
func (s *Store) AdminListCategories(keyword string, page, pageSize int) ([]model.Category, int64, error) {
	q := s.db.Model(&model.Category{})
	applyKeyword := func(db *gorm.DB) *gorm.DB {
		if kw := strings.TrimSpace(keyword); kw != "" {
			like := termQueryLike(kw)
			return db.Where("LOWER(name) LIKE ? OR LOWER(slug) LIKE ? OR LOWER(description) LIKE ?", like, like, like)
		}
		return db
	}
	q = applyKeyword(q)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count categories")
	}
	var categories []model.Category
	listQ := applyKeyword(s.db.Model(&model.Category{}))
	err := listQ.Select("categories.*, COUNT(DISTINCT posts.id) AS post_count").
		Joins("LEFT JOIN post_categories pc_count ON pc_count.category_id = categories.id").
		Joins("LEFT JOIN posts ON posts.id = pc_count.post_id AND posts.post_type = ?", model.PostTypePost).
		Group("categories.id").Order("categories.name ASC").
		Offset((page-1)*pageSize).Limit(pageSize).Find(&categories).Error
	return categories, total, errors.Wrap(err, "admin list categories")
}

// AdminListTags 返回后台标签列表(支持搜索与分页)。
func (s *Store) AdminListTags(keyword string, page, pageSize int) ([]model.Tag, int64, error) {
	q := s.db.Model(&model.Tag{})
	applyKeyword := func(db *gorm.DB) *gorm.DB {
		if kw := strings.TrimSpace(keyword); kw != "" {
			like := termQueryLike(kw)
			return db.Where("LOWER(name) LIKE ? OR LOWER(slug) LIKE ?", like, like)
		}
		return db
	}
	q = applyKeyword(q)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count tags")
	}
	var tags []model.Tag
	listQ := applyKeyword(s.db.Model(&model.Tag{}))
	err := listQ.Select("tags.*, COUNT(DISTINCT posts.id) AS post_count").
		Joins("LEFT JOIN post_tags pt_count ON pt_count.tag_id = tags.id").
		Joins("LEFT JOIN posts ON posts.id = pt_count.post_id AND posts.post_type = ?", model.PostTypePost).
		Group("tags.id").Order("tags.name ASC").
		Offset((page-1)*pageSize).Limit(pageSize).Find(&tags).Error
	return tags, total, errors.Wrap(err, "admin list tags")
}

// AdminPostsByIDs 按 ID 批量返回后台所需的文章/页面基础信息。
func (s *Store) AdminPostsByIDs(ids []uint) (map[uint]model.Post, error) {
	result := make(map[uint]model.Post, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	uniq := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return result, nil
	}
	var posts []model.Post
	err := s.db.Select("id", "title", "slug", "post_type", "published_at", "author_id").
		Preload("Categories").Preload("Author").
		Where("id IN ?", uniq).Find(&posts).Error
	if err != nil {
		return nil, errors.Wrap(err, "admin posts by ids")
	}
	for _, post := range posts {
		result[post.ID] = post
	}
	return result, nil
}

// SetCommentStatus 更新评论状态(approved/pending/spam)。
func (s *Store) SetCommentStatus(id uint, status string) error {
	return errors.Wrap(
		s.db.Model(&model.Comment{}).Where("id = ?", id).Update("status", status).Error,
		"set comment status")
}

// DeleteComment 删除评论。
func (s *Store) DeleteComment(id uint) error {
	return s.softDeleteComments([]uint{id})
}

// UpdateCommentFields 修改评论内容/作者元信息(管理员编辑)。
func (s *Store) UpdateCommentFields(id uint, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return errors.Wrap(
		s.db.Model(&model.Comment{}).Where("id = ?", id).Updates(fields).Error,
		"update comment fields")
}

// BatchSetCommentStatus 批量更新评论状态。
func (s *Store) BatchSetCommentStatus(ids []uint, status string) error {
	if len(ids) == 0 {
		return nil
	}
	return errors.Wrap(
		s.db.Model(&model.Comment{}).Where("id IN ?", ids).Update("status", status).Error,
		"batch set comment status")
}

// BatchDeleteComments 批量删除评论。
func (s *Store) BatchDeleteComments(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return s.softDeleteComments(ids)
}

func (s *Store) softDeleteComments(ids []uint) error {
	allIDs, err := s.commentTreeIDs(ids)
	if err != nil {
		return err
	}
	if len(allIDs) == 0 {
		return nil
	}
	return errors.Wrap(
		s.db.Model(&model.Comment{}).
			Where("id IN ?", allIDs).
			Update("status", model.CommentDeleted).Error,
		"soft delete comments",
	)
}

func (s *Store) commentTreeIDs(ids []uint) ([]uint, error) {
	seen := make(map[uint]struct{}, len(ids))
	queue := append([]uint(nil), ids...)
	for len(queue) > 0 {
		frontier := make([]uint, 0, len(queue))
		for _, id := range queue {
			if id == 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			frontier = append(frontier, id)
		}
		if len(frontier) == 0 {
			break
		}
		var children []uint
		if err := s.db.Model(&model.Comment{}).
			Where("parent_id IN ?", frontier).
			Pluck("id", &children).
			Error; err != nil {
			return nil, errors.Wrap(err, "list child comments")
		}
		queue = children
	}
	allIDs := make([]uint, 0, len(seen))
	for id := range seen {
		allIDs = append(allIDs, id)
	}
	sort.Slice(allIDs, func(i, j int) bool { return allIDs[i] < allIDs[j] })
	return allIDs, nil
}

// CommentCounts 是后台评论各状态计数。
type CommentCounts struct {
	Pending  int64
	Approved int64
	Spam     int64
}

// AdminCommentCounts 返回三种状态的评论数(后台标签角标)。
func (s *Store) AdminCommentCounts() CommentCounts {
	var cc CommentCounts
	s.db.Model(&model.Comment{}).Where("status = ?", model.CommentPending).Count(&cc.Pending)
	s.db.Model(&model.Comment{}).Where("status = ?", model.CommentApproved).Count(&cc.Approved)
	s.db.Model(&model.Comment{}).Where("status = ?", model.CommentSpam).Count(&cc.Spam)
	return cc
}

// PendingCommentCount 返回待审评论数(后台角标)。
func (s *Store) PendingCommentCount() int64 {
	var n int64
	s.db.Model(&model.Comment{}).Where("status = ?", model.CommentPending).Count(&n)
	return n
}

// SaveUpload 创建上传元数据记录。
func (s *Store) SaveUpload(u *model.Upload) error {
	return errors.Wrap(s.db.Create(u).Error, "save upload")
}

// ListUploads 分页返回上传文件(最新在前)。
func (s *Store) ListUploads(page, pageSize int) ([]model.Upload, int64, error) {
	q := s.db.Model(&model.Upload{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, "count uploads")
	}
	var uploads []model.Upload
	err := q.Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&uploads).Error
	return uploads, total, errors.Wrap(err, "list uploads")
}

// GetUpload 按 ID 取上传元数据。
func (s *Store) GetUpload(id uint) (*model.Upload, error) {
	var u model.Upload
	if err := s.db.First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// DeleteUpload 删除上传元数据记录。
func (s *Store) DeleteUpload(id uint) error {
	return errors.Wrap(s.db.Delete(&model.Upload{}, id).Error, "delete upload")
}
