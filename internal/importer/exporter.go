package importer

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"gorm.io/gorm"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
)

const exportTimeLayout = "2006-01-02 15:04:05"

// ExportOptions 控制导出哪些内容。
type ExportOptions struct {
	Posts    bool
	Pages    bool
	Comments bool
	Settings bool

	SiteTitle string
	SiteURL   string
}

// ExportStats 是导出结果统计。
type ExportStats struct {
	Posts      int
	Pages      int
	Comments   int
	Categories int
	Tags       int
	Settings   int
}

// ExportXML 导出可被当前导入逻辑重新导入的 XML。
func ExportXML(db *gorm.DB, opts ExportOptions) ([]byte, *ExportStats, error) {
	if db == nil {
		return nil, nil, errors.New("nil db")
	}
	if opts.Comments && !opts.Posts && !opts.Pages {
		return nil, nil, errors.New("仅导出评论无法形成可回导 XML，请至少同时选择文章或页面")
	}

	ctx, err := collectExportContext(db, opts)
	if err != nil {
		return nil, nil, err
	}
	rss := buildExportRSS(ctx, opts)
	b, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		return nil, nil, errors.Wrap(err, "marshal export xml")
	}
	buf := bytes.NewBufferString(xml.Header)
	buf.Write(b)
	buf.WriteByte('\n')
	return buf.Bytes(), ctx.stats, nil
}

type exportContext struct {
	posts      []model.Post
	postIDs    map[uint]bool
	comments   map[uint][]model.Comment
	categories []model.Category
	tags       []model.Tag
	settings   []model.Setting
	stats      *ExportStats
}

func collectExportContext(db *gorm.DB, opts ExportOptions) (*exportContext, error) {
	ctx := &exportContext{postIDs: map[uint]bool{}, comments: map[uint][]model.Comment{}, stats: &ExportStats{}}
	if opts.Posts || opts.Pages {
		var posts []model.Post
		q := db.Preload("Categories").Preload("Tags").Order("id ASC")
		switch {
		case opts.Posts && opts.Pages:
			q = q.Where("post_type IN ?", []string{model.PostTypePost, model.PostTypePage})
		case opts.Posts:
			q = q.Where("post_type = ?", model.PostTypePost)
		case opts.Pages:
			q = q.Where("post_type = ?", model.PostTypePage)
		}
		if err := q.Find(&posts).Error; err != nil {
			return nil, errors.Wrap(err, "list export posts")
		}
		ctx.posts = posts
		catMap := map[uint]model.Category{}
		tagMap := map[uint]model.Tag{}
		for _, p := range posts {
			ctx.postIDs[p.ID] = true
			if p.PostType == model.PostTypePost {
				ctx.stats.Posts++
			} else if p.PostType == model.PostTypePage {
				ctx.stats.Pages++
			}
			for _, c := range p.Categories {
				catMap[c.ID] = c
			}
			for _, t := range p.Tags {
				tagMap[t.ID] = t
			}
		}
		ctx.categories = mapsToSortedCategories(catMap)
		ctx.tags = mapsToSortedTags(tagMap)
		ctx.stats.Categories = len(ctx.categories)
		ctx.stats.Tags = len(ctx.tags)
	}
	if opts.Comments {
		var comments []model.Comment
		if len(ctx.postIDs) > 0 {
			ids := make([]uint, 0, len(ctx.postIDs))
			for id := range ctx.postIDs {
				ids = append(ids, id)
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			if err := db.Order("id ASC").Where("post_id IN ? AND status <> ?", ids, model.CommentDeleted).Find(&comments).Error; err != nil {
				return nil, errors.Wrap(err, "list export comments")
			}
			for _, c := range comments {
				ctx.comments[c.PostID] = append(ctx.comments[c.PostID], c)
				ctx.stats.Comments++
			}
		}
	}
	if opts.Settings {
		var settings []model.Setting
		if err := db.Order("key ASC").Find(&settings).Error; err != nil {
			return nil, errors.Wrap(err, "list export settings")
		}
		ctx.settings = settings
		ctx.stats.Settings = len(settings)
	}
	return ctx, nil
}

func mapsToSortedCategories(m map[uint]model.Category) []model.Category {
	out := make([]model.Category, 0, len(m))
	for _, c := range m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func mapsToSortedTags(m map[uint]model.Tag) []model.Tag {
	out := make([]model.Tag, 0, len(m))
	for _, t := range m {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

type exportRSS struct {
	XMLName      xml.Name         `xml:"rss"`
	Version      string           `xml:"version,attr"`
	XMLNSExcerpt string           `xml:"xmlns:excerpt,attr,omitempty"`
	XMLNSContent string           `xml:"xmlns:content,attr,omitempty"`
	XMLNSWFW     string           `xml:"xmlns:wfw,attr,omitempty"`
	XMLNSDC      string           `xml:"xmlns:dc,attr,omitempty"`
	XMLNSWP      string           `xml:"xmlns:wp,attr,omitempty"`
	Channel      exportXMLChannel `xml:"channel"`
}

type exportXMLChannel struct {
	Title      string              `xml:"title"`
	Link       string              `xml:"link"`
	Categories []exportXMLCategory `xml:"wp:category,omitempty"`
	Tags       []exportXMLTag      `xml:"wp:tag,omitempty"`
	Settings   []exportXMLSetting  `xml:"blog_setting,omitempty"`
	Items      []exportXMLItem     `xml:"item,omitempty"`
}

type exportXMLCategory struct {
	TermID      int    `xml:"wp:term_id"`
	Nicename    string `xml:"wp:category_nicename"`
	Parent      string `xml:"wp:category_parent,omitempty"`
	Name        string `xml:"wp:cat_name"`
	Description string `xml:"wp:category_description,omitempty"`
}

type exportXMLTag struct {
	TermID int    `xml:"wp:term_id"`
	Slug   string `xml:"wp:tag_slug"`
	Name   string `xml:"wp:tag_name"`
}

type exportXMLSetting struct {
	Key   string `xml:"blog_key"`
	Value string `xml:"blog_value"`
}

type exportXMLCategoryRef struct {
	Domain   string `xml:"domain,attr"`
	Nicename string `xml:"nicename,attr"`
	Name     string `xml:",cdata"`
}

type exportXMLMeta struct {
	Key   string `xml:"wp:meta_key"`
	Value string `xml:"wp:meta_value"`
}

type exportXMLComment struct {
	ID       int    `xml:"wp:comment_id"`
	Author   string `xml:"wp:comment_author"`
	Email    string `xml:"wp:comment_author_email,omitempty"`
	URL      string `xml:"wp:comment_author_url,omitempty"`
	IP       string `xml:"wp:comment_author_IP,omitempty"`
	Date     string `xml:"wp:comment_date,omitempty"`
	Content  string `xml:"wp:comment_content"`
	Approved string `xml:"wp:comment_approved"`
	Type     string `xml:"wp:comment_type,omitempty"`
	Parent   int    `xml:"wp:comment_parent,omitempty"`
}

type exportXMLItem struct {
	Title         string                 `xml:"title"`
	Link          string                 `xml:"link,omitempty"`
	Creator       string                 `xml:"dc:creator,omitempty"`
	Content       string                 `xml:"content:encoded"`
	Excerpt       string                 `xml:"excerpt:encoded,omitempty"`
	PostID        int                    `xml:"wp:post_id"`
	PostDate      string                 `xml:"wp:post_date,omitempty"`
	PostModified  string                 `xml:"wp:post_modified,omitempty"`
	PostName      string                 `xml:"wp:post_name,omitempty"`
	Status        string                 `xml:"wp:status"`
	PostType      string                 `xml:"wp:post_type"`
	MenuOrder     int                    `xml:"wp:menu_order,omitempty"`
	CommentStatus string                 `xml:"wp:comment_status,omitempty"`
	Categories    []exportXMLCategoryRef `xml:"category,omitempty"`
	Metas         []exportXMLMeta        `xml:"wp:postmeta,omitempty"`
	Comments      []exportXMLComment     `xml:"wp:comment,omitempty"`
}

func buildExportRSS(ctx *exportContext, opts ExportOptions) exportRSS {
	ch := exportXMLChannel{
		Title: strings.TrimSpace(opts.SiteTitle),
		Link:  strings.TrimSpace(opts.SiteURL),
	}
	if ch.Title == "" {
		ch.Title = "blog export"
	}
	for _, c := range ctx.categories {
		ch.Categories = append(ch.Categories, exportXMLCategory{
			TermID: int(c.ID), Nicename: c.Slug, Parent: categoryParentSlug(ctx.categories, c.ParentID),
			Name: c.Name, Description: c.Description,
		})
	}
	for _, t := range ctx.tags {
		ch.Tags = append(ch.Tags, exportXMLTag{TermID: int(t.ID), Slug: t.Slug, Name: t.Name})
	}
	for _, s := range ctx.settings {
		ch.Settings = append(ch.Settings, exportXMLSetting{Key: s.Key, Value: s.Value})
	}
	for _, p := range ctx.posts {
		item := exportXMLItem{
			Title:         p.Title,
			Link:          exportPostLink(&p, opts.SiteURL),
			Content:       p.Content,
			Excerpt:       p.Excerpt,
			PostID:        int(p.ID),
			PostDate:      formatWPTime(p.PublishedAt),
			PostModified:  formatWPTime(p.ModifiedAt),
			PostName:      p.Slug,
			Status:        exportPostStatus(p.Status),
			PostType:      p.PostType,
			MenuOrder:     p.MenuOrder,
			CommentStatus: firstNonEmpty(p.CommentStatus, "open"),
			Metas:         []exportXMLMeta{{Key: "views", Value: strconv.FormatInt(p.Views, 10)}},
		}
		for _, c := range p.Categories {
			item.Categories = append(item.Categories, exportXMLCategoryRef{Domain: "category", Nicename: c.Slug, Name: c.Name})
		}
		for _, t := range p.Tags {
			item.Categories = append(item.Categories, exportXMLCategoryRef{Domain: "post_tag", Nicename: t.Slug, Name: t.Name})
		}
		for _, c := range ctx.comments[p.ID] {
			item.Comments = append(item.Comments, exportXMLComment{
				ID:       int(c.ID),
				Author:   c.Author,
				Email:    c.Email,
				URL:      c.URL,
				IP:       c.IP,
				Date:     formatWPTime(c.CreatedAt),
				Content:  c.Content,
				Approved: exportCommentStatus(c.Status),
				Parent:   int(c.ParentID),
			})
		}
		ch.Items = append(ch.Items, item)
	}
	return exportRSS{
		Version:      "2.0",
		XMLNSExcerpt: "http://wordpress.org/export/1.2/excerpt/",
		XMLNSContent: "http://purl.org/rss/1.0/modules/content/",
		XMLNSWFW:     "http://wellformedweb.org/CommentAPI/",
		XMLNSDC:      "http://purl.org/dc/elements/1.1/",
		XMLNSWP:      "http://wordpress.org/export/1.2/",
		Channel:      ch,
	}
}

func categoryParentSlug(all []model.Category, parentID uint) string {
	if parentID == 0 {
		return ""
	}
	for _, c := range all {
		if c.ID == parentID {
			return c.Slug
		}
	}
	return ""
}

func exportPostLink(p *model.Post, siteURL string) string {
	base := strings.TrimRight(strings.TrimSpace(siteURL), "/")
	path := permalink.Post(p)
	if p.PostType == model.PostTypePage {
		path = permalink.Page(p)
	}
	if base == "" {
		return path
	}
	return base + path
}

func exportPostStatus(status string) string {
	if status == model.StatusPublished {
		return "publish"
	}
	return "draft"
}

func exportCommentStatus(status string) string {
	switch status {
	case model.CommentApproved:
		return "1"
	case model.CommentSpam:
		return "spam"
	default:
		return "0"
	}
}

func formatWPTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(exportTimeLayout)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func ExportFilename() string {
	return fmt.Sprintf("blog-export-%s.xml", time.Now().Format("20060102-150405"))
}
