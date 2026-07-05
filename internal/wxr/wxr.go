// Package wxr 解析 WordPress eXtended RSS(WXR)导出文件。
package wxr

import (
	"encoding/xml"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

// WordPress 导出的 post_date 形如 "2012-12-06 00:00:18"(站点本地时间)。
const wpTimeLayout = "2006-01-02 15:04:05"

// Document 是解析后的 WXR 文档。
type Document struct {
	Title      string
	Link       string
	Categories []Category
	Tags       []Tag
	Authors    []Author
	Settings   []Setting
	Items      []Item
}

// Author 对应 wp:author。
type Author struct {
	ID          int
	Login       string
	Email       string
	DisplayName string
}

// Category 对应 channel 级 wp:category(含层级)。
type Category struct {
	TermID      int
	Nicename    string // slug
	Parent      string // 父分类的 nicename,空表示顶级
	Name        string
	Description string
}

// Tag 对应 channel 级 wp:tag。
type Tag struct {
	TermID int
	Slug   string
	Name   string
}

// TermRef 是 item 内引用的分类/标签。
type TermRef struct {
	Domain   string // category / post_tag
	Nicename string
	Name     string
}

// Meta 是 item 的 postmeta 键值对。
type Meta struct {
	Key   string
	Value string
}

// Setting 是站点级导入导出设置项。
type Setting struct {
	Key   string
	Value string
}

// Comment 对应 wp:comment。
type Comment struct {
	ID       int
	UserID   int
	Author   string
	Email    string
	URL      string
	IP       string
	Date     time.Time
	Content  string
	Approved string // "1" 已审核
	Type     string // comment / pingback...
	Parent   int
}

// Item 是一条内容(文章/页面/菜单等),由 PostType 区分。
type Item struct {
	Title         string
	Link          string
	Creator       string // dc:creator,作者 login
	PostID        int
	PostName      string // slug
	PostType      string
	Status        string
	Content       string
	Excerpt       string
	PostDate      time.Time
	PostModified  time.Time
	MenuOrder     int
	CommentStatus string
	Terms         []TermRef
	Metas         []Meta
	Comments      []Comment
}

// --- 以下为 XML 解码结构(贴合 WXR schema)---

type xmlRSS struct {
	Channel xmlChannel `xml:"channel"`
}

type xmlChannel struct {
	Title      string        `xml:"title"`
	Link       string        `xml:"link"`
	Authors    []xmlAuthor   `xml:"http://wordpress.org/export/1.2/ author"`
	Categories []xmlCategory `xml:"http://wordpress.org/export/1.2/ category"`
	Tags       []xmlTag      `xml:"http://wordpress.org/export/1.2/ tag"`
	Settings   []xmlSetting  `xml:"wenlog_setting"`
	Items      []xmlItem     `xml:"item"`
}

type xmlSetting struct {
	Key   string `xml:"wenlog_key"`
	Value string `xml:"wenlog_value"`
}

type xmlAuthor struct {
	ID          int    `xml:"http://wordpress.org/export/1.2/ author_id"`
	Login       string `xml:"http://wordpress.org/export/1.2/ author_login"`
	Email       string `xml:"http://wordpress.org/export/1.2/ author_email"`
	DisplayName string `xml:"http://wordpress.org/export/1.2/ author_display_name"`
}

type xmlCategory struct {
	TermID      int    `xml:"http://wordpress.org/export/1.2/ term_id"`
	Nicename    string `xml:"http://wordpress.org/export/1.2/ category_nicename"`
	Parent      string `xml:"http://wordpress.org/export/1.2/ category_parent"`
	Name        string `xml:"http://wordpress.org/export/1.2/ cat_name"`
	Description string `xml:"http://wordpress.org/export/1.2/ category_description"`
}

type xmlTag struct {
	TermID int    `xml:"http://wordpress.org/export/1.2/ term_id"`
	Slug   string `xml:"http://wordpress.org/export/1.2/ tag_slug"`
	Name   string `xml:"http://wordpress.org/export/1.2/ tag_name"`
}

type xmlCat struct {
	Domain   string `xml:"domain,attr"`
	Nicename string `xml:"nicename,attr"`
	Name     string `xml:",chardata"`
}

type xmlMeta struct {
	Key   string `xml:"http://wordpress.org/export/1.2/ meta_key"`
	Value string `xml:"http://wordpress.org/export/1.2/ meta_value"`
}

type xmlComment struct {
	ID       int    `xml:"http://wordpress.org/export/1.2/ comment_id"`
	UserID   int    `xml:"http://wordpress.org/export/1.2/ comment_user_id"`
	Author   string `xml:"http://wordpress.org/export/1.2/ comment_author"`
	Email    string `xml:"http://wordpress.org/export/1.2/ comment_author_email"`
	URL      string `xml:"http://wordpress.org/export/1.2/ comment_author_url"`
	IP       string `xml:"http://wordpress.org/export/1.2/ comment_author_IP"`
	Date     string `xml:"http://wordpress.org/export/1.2/ comment_date"`
	DateGMT  string `xml:"http://wordpress.org/export/1.2/ comment_date_gmt"`
	Content  string `xml:"http://wordpress.org/export/1.2/ comment_content"`
	Approved string `xml:"http://wordpress.org/export/1.2/ comment_approved"`
	Type     string `xml:"http://wordpress.org/export/1.2/ comment_type"`
	Parent   int    `xml:"http://wordpress.org/export/1.2/ comment_parent"`
}

type xmlItem struct {
	Title         string       `xml:"title"`
	Link          string       `xml:"link"`
	PubDate       string       `xml:"pubDate"`
	Creator       string       `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Content       string       `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	Excerpt       string       `xml:"http://wordpress.org/export/1.2/excerpt/ encoded"`
	PostID        int          `xml:"http://wordpress.org/export/1.2/ post_id"`
	PostDate      string       `xml:"http://wordpress.org/export/1.2/ post_date"`
	PostDateGMT   string       `xml:"http://wordpress.org/export/1.2/ post_date_gmt"`
	PostModified  string       `xml:"http://wordpress.org/export/1.2/ post_modified"`
	ModifiedGMT   string       `xml:"http://wordpress.org/export/1.2/ post_modified_gmt"`
	PostName      string       `xml:"http://wordpress.org/export/1.2/ post_name"`
	Status        string       `xml:"http://wordpress.org/export/1.2/ status"`
	PostType      string       `xml:"http://wordpress.org/export/1.2/ post_type"`
	MenuOrder     int          `xml:"http://wordpress.org/export/1.2/ menu_order"`
	CommentStatus string       `xml:"http://wordpress.org/export/1.2/ comment_status"`
	Cats          []xmlCat     `xml:"category"`
	Metas         []xmlMeta    `xml:"http://wordpress.org/export/1.2/ postmeta"`
	Comments      []xmlComment `xml:"http://wordpress.org/export/1.2/ comment"`
}

// Parse 从 reader 解析 WXR XML。
func Parse(r io.Reader) (*Document, error) {
	var rss xmlRSS
	dec := xml.NewDecoder(r)
	if err := dec.Decode(&rss); err != nil {
		return nil, errors.Wrap(err, "decode wxr xml")
	}

	doc := &Document{
		Title: rss.Channel.Title,
		Link:  rss.Channel.Link,
	}
	for _, a := range rss.Channel.Authors {
		doc.Authors = append(doc.Authors, Author{ID: a.ID, Login: a.Login, Email: a.Email, DisplayName: a.DisplayName})
	}
	for _, c := range rss.Channel.Categories {
		doc.Categories = append(doc.Categories, Category{
			TermID: c.TermID, Nicename: c.Nicename, Parent: c.Parent,
			Name: c.Name, Description: c.Description,
		})
	}
	for _, t := range rss.Channel.Tags {
		doc.Tags = append(doc.Tags, Tag{TermID: t.TermID, Slug: t.Slug, Name: t.Name})
	}
	for _, s := range rss.Channel.Settings {
		doc.Settings = append(doc.Settings, Setting{Key: s.Key, Value: s.Value})
	}
	for _, it := range rss.Channel.Items {
		postDate := parseWPTime(it.PostDate)
		if postDate.IsZero() {
			postDate = parseWPGMTTime(it.PostDateGMT)
		}
		if postDate.IsZero() {
			postDate = parseRSSPubDate(it.PubDate)
		}
		postModified := parseWPTime(it.PostModified)
		if postModified.IsZero() {
			postModified = parseWPGMTTime(it.ModifiedGMT)
		}
		item := Item{
			Title: it.Title, Link: it.Link, Creator: it.Creator, PostID: it.PostID,
			PostName: it.PostName, PostType: it.PostType, Status: it.Status,
			Content: it.Content, Excerpt: it.Excerpt, MenuOrder: it.MenuOrder,
			CommentStatus: it.CommentStatus,
			PostDate:      postDate,
			PostModified:  postModified,
		}
		for _, c := range it.Cats {
			item.Terms = append(item.Terms, TermRef{Domain: c.Domain, Nicename: c.Nicename, Name: c.Name})
		}
		for _, m := range it.Metas {
			item.Metas = append(item.Metas, Meta{Key: m.Key, Value: m.Value})
		}
		for _, cm := range it.Comments {
			commentDate := parseWPTime(cm.Date)
			if commentDate.IsZero() {
				commentDate = parseWPGMTTime(cm.DateGMT)
			}
			item.Comments = append(item.Comments, Comment{
				ID: cm.ID, UserID: cm.UserID, Author: cm.Author, Email: cm.Email, URL: cm.URL,
				IP: cm.IP, Date: commentDate, Content: cm.Content,
				Approved: cm.Approved, Type: cm.Type, Parent: cm.Parent,
			})
		}
		doc.Items = append(doc.Items, item)
	}
	return doc, nil
}

// ParseFile 解析指定路径的 WXR 文件。
func ParseFile(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrap(err, "open wxr file")
	}
	defer f.Close()
	return Parse(f)
}

// parseWPTime 解析 WP 时间字符串,失败返回零值。
func parseWPTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" || s == "0000-00-00 00:00:00" {
		return time.Time{}
	}
	t, err := time.ParseInLocation(wpTimeLayout, s, time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseWPGMTTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" || s == "0000-00-00 00:00:00" {
		return time.Time{}
	}
	t, err := time.ParseInLocation(wpTimeLayout, s, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseRSSPubDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC1123Z, time.RFC822Z, time.RFC822, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
