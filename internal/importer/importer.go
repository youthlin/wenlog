// Package importer 提供后台可复用的 WordPress WXR 导入能力。
package importer

import (
	"io"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/wxr"
)

// Options 是导入选项。
type Options struct {
	// TargetUserID 指定导入后的文章/页面归属用户。
	TargetUserID uint
	// IncludeDrafts 为 false 时跳过非 publish 内容。
	IncludeDrafts bool
}

// Stats 是导入结果统计。
type Stats struct {
	Posts      int
	Pages      int
	Comments   int
	Categories int
	Tags       int
	Settings   int
}

// ImportReader 解析并导入上传的 WXR XML。
func ImportReader(db *gorm.DB, r io.Reader, opts Options) (*Stats, error) {
	doc, err := wxr.Parse(r)
	if err != nil {
		return nil, err
	}
	return ImportDocument(db, doc, opts)
}

// ImportDocument 把解析后的 WXR 文档 upsert 到数据库。
func ImportDocument(db *gorm.DB, doc *wxr.Document, opts Options) (*Stats, error) {
	if db == nil {
		return nil, errors.New("nil db")
	}
	if doc == nil {
		return nil, errors.New("nil wxr document")
	}
	if opts.TargetUserID == 0 {
		return nil, errors.New("target user is required")
	}

	var s Stats
	if err := importTerms(db, doc, &s); err != nil {
		return nil, err
	}
	if err := importSettings(db, doc, &s); err != nil {
		return nil, err
	}
	if err := importItems(db, doc, opts, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func importSettings(db *gorm.DB, doc *wxr.Document, s *Stats) error {
	for _, item := range doc.Settings {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		st := model.Setting{Key: key, Value: item.Value}
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&st).Error; err != nil {
			return errors.Wrap(err, "import setting")
		}
		s.Settings++
	}
	return nil
}

// importTerms 导入分类(含父子层级)和标签。
func importTerms(db *gorm.DB, doc *wxr.Document, s *Stats) error {
	bySlug := map[string]int{}
	for _, c := range doc.Categories {
		bySlug[c.Nicename] = c.TermID
	}
	for _, c := range doc.Categories {
		var parentID uint
		if c.Parent != "" {
			if pid, ok := bySlug[c.Parent]; ok {
				parentID = uint(pid)
			}
		}
		cat := model.Category{
			ID: uint(c.TermID), Name: c.Name, Slug: c.Nicename,
			Description: c.Description, ParentID: parentID,
		}
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&cat).Error; err != nil {
			return errors.Wrap(err, "import category")
		}
		s.Categories++
	}
	seenTagSlug := map[string]bool{}
	for _, t := range doc.Tags {
		tag := model.Tag{ID: uint(t.TermID), Name: t.Name, Slug: t.Slug}
		if seenTagSlug[t.Slug] {
			continue
		}
		seenTagSlug[t.Slug] = true
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&tag).Error; err != nil {
			return errors.Wrap(err, "import tag")
		}
		s.Tags++
	}
	return nil
}

// importItems 导入文章和页面(及其评论、关联、meta)。
func importItems(db *gorm.DB, doc *wxr.Document, opts Options, s *Stats) error {
	for i := range doc.Items {
		it := &doc.Items[i]
		if it.PostType != model.PostTypePost && it.PostType != model.PostTypePage {
			continue
		}
		if it.Status != "publish" && !opts.IncludeDrafts {
			continue
		}
		if err := importOneItem(db, it, opts.TargetUserID, s); err != nil {
			return errors.Wrapf(err, "import item id=%d", it.PostID)
		}
	}
	return nil
}

func importOneItem(db *gorm.DB, it *wxr.Item, targetUserID uint, s *Stats) error {
	content := wxr.CleanContent(it.Content)
	excerpt := strings.TrimSpace(it.Excerpt)

	status := model.StatusPublished
	if it.Status != "publish" {
		status = model.StatusDraft
	}

	var views int64
	for _, m := range it.Metas {
		switch m.Key {
		case "views":
			views, _ = strconv.ParseInt(strings.TrimSpace(m.Value), 10, 64)
		}
	}

	commentStatus := strings.TrimSpace(it.CommentStatus)
	if commentStatus == "" {
		commentStatus = "open"
	}

	post := model.Post{
		ID: uint(it.PostID), Title: it.Title, Slug: it.PostName,
		Content: content, ContentMD: content, Excerpt: excerpt, Status: status,
		AuthorID: targetUserID,
		PostType: it.PostType, ContentFormat: model.FormatHTML,
		CommentStatus: commentStatus, Views: views, MenuOrder: it.MenuOrder,
		PublishedAt: it.PostDate, ModifiedAt: it.PostModified,
	}
	if err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}}, UpdateAll: true,
	}).Create(&post).Error; err != nil {
		return errors.Wrap(err, "upsert post")
	}

	if err := associateTerms(db, &post, it); err != nil {
		return err
	}

	for _, c := range it.Comments {
		if c.Type != "" && c.Type != "comment" {
			continue
		}
		cstatus := model.CommentPending
		if c.Approved == "1" {
			cstatus = model.CommentApproved
		} else if c.Approved == "spam" {
			cstatus = model.CommentSpam
		}
		cm := model.Comment{
			ID: uint(c.ID), PostID: uint(it.PostID), ParentID: uint(c.Parent),
			Author: c.Author, Email: c.Email, URL: c.URL, IP: c.IP,
			Content: c.Content, Status: cstatus, NotifyOnReply: false,
			CreatedAt: c.Date,
		}
		if err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}}, UpdateAll: true,
		}).Create(&cm).Error; err != nil {
			return errors.Wrap(err, "upsert comment")
		}
		s.Comments++
	}

	if it.PostType == model.PostTypePost {
		s.Posts++
	} else {
		s.Pages++
	}
	return nil
}

func associateTerms(db *gorm.DB, post *model.Post, it *wxr.Item) error {
	var cats []model.Category
	var tags []model.Tag
	for _, ref := range it.Terms {
		switch ref.Domain {
		case "category":
			var c model.Category
			if err := db.Where("slug = ?", ref.Nicename).First(&c).Error; err == nil {
				cats = append(cats, c)
			}
		case "post_tag":
			var t model.Tag
			if err := db.Where("slug = ?", ref.Nicename).First(&t).Error; err == nil {
				tags = append(tags, t)
			}
		}
	}
	if err := db.Model(post).Association("Categories").Replace(cats); err != nil {
		return errors.Wrap(err, "associate categories")
	}
	if err := db.Model(post).Association("Tags").Replace(tags); err != nil {
		return errors.Wrap(err, "associate tags")
	}
	return nil
}
