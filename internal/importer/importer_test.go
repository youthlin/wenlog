package importer

import (
	"strings"
	"testing"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return st
}

func TestImportReaderAssignsTargetUserAndUpserts(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	if err := db.Create(&model.User{ID: 7, Username: "admin", DisplayName: "管理员", Email: "admin@example.com"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&model.Post{ID: 123, Title: "旧标题", AuthorID: 1, PostType: model.PostTypePost, Status: model.StatusPublished}).Error; err != nil {
		t.Fatalf("seed old post: %v", err)
	}

	xml := `<?xml version="1.0" encoding="UTF-8" ?>
<rss version="2.0"
	xmlns:excerpt="http://wordpress.org/export/1.2/excerpt/"
	xmlns:content="http://purl.org/rss/1.0/modules/content/"
	xmlns:wfw="http://wellformedweb.org/CommentAPI/"
	xmlns:dc="http://purl.org/dc/elements/1.1/"
	xmlns:wp="http://wordpress.org/export/1.2/">
  <channel>
    <title>test</title>
    <link>https://example.com</link>
    <blog_setting><blog_key>site_name</blog_key><blog_value>测试站</blog_value></blog_setting>
    <wp:category><wp:term_id>10</wp:term_id><wp:category_nicename>go</wp:category_nicename><wp:cat_name>Go</wp:cat_name><wp:category_parent></wp:category_parent><wp:category_description></wp:category_description></wp:category>
    <wp:tag><wp:term_id>20</wp:term_id><wp:tag_slug>backend</wp:tag_slug><wp:tag_name>Backend</wp:tag_name></wp:tag>
    <item>
      <title>新标题</title>
      <dc:creator>someone</dc:creator>
      <content:encoded><![CDATA[<p>Hello</p><!--more--><p>World</p>]]></content:encoded>
      <excerpt:encoded><![CDATA[摘要]]></excerpt:encoded>
      <wp:post_id>123</wp:post_id>
      <wp:post_date>2024-01-02 03:04:05</wp:post_date>
      <wp:post_modified>2024-01-03 03:04:05</wp:post_modified>
      <wp:post_name>hello</wp:post_name>
      <wp:status>publish</wp:status>
      <wp:post_type>post</wp:post_type>
      <wp:menu_order>0</wp:menu_order>
      <wp:comment_status>open</wp:comment_status>
      <category domain="category" nicename="go"><![CDATA[Go]]></category>
      <category domain="post_tag" nicename="backend"><![CDATA[Backend]]></category>
      <wp:postmeta><wp:meta_key>views</wp:meta_key><wp:meta_value>42</wp:meta_value></wp:postmeta>
      <wp:comment>
        <wp:comment_id>900</wp:comment_id>
        <wp:comment_author>Alice</wp:comment_author>
        <wp:comment_author_email>alice@example.com</wp:comment_author_email>
        <wp:comment_author_url>https://example.com/alice</wp:comment_author_url>
        <wp:comment_author_IP>127.0.0.1</wp:comment_author_IP>
        <wp:comment_date>2024-01-04 03:04:05</wp:comment_date>
        <wp:comment_content><![CDATA[评论]]></wp:comment_content>
        <wp:comment_approved>1</wp:comment_approved>
        <wp:comment_type></wp:comment_type>
        <wp:comment_parent>0</wp:comment_parent>
      </wp:comment>
    </item>
  </channel>
</rss>`

	stats, err := ImportReader(db, strings.NewReader(xml), Options{TargetUserID: 7, IncludeDrafts: true})
	if err != nil {
		t.Fatalf("import xml: %v", err)
	}
	if stats.Posts != 1 || stats.Categories != 1 || stats.Tags != 1 || stats.Comments != 1 || stats.Settings != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	p, err := st.AdminGetPost(123)
	if err != nil {
		t.Fatalf("load imported post: %v", err)
	}
	if p.Title != "新标题" {
		t.Fatalf("title = %q, want 新标题", p.Title)
	}
	if p.AuthorID != 7 {
		t.Fatalf("author_id = %d, want 7", p.AuthorID)
	}
	if p.ContentFormat != model.FormatHTML {
		t.Fatalf("content_format = %q, want %q", p.ContentFormat, model.FormatHTML)
	}
	if p.Views != 42 {
		t.Fatalf("views = %d, want 42", p.Views)
	}
	if len(p.Categories) != 1 || p.Categories[0].Slug != "go" {
		t.Fatalf("categories = %+v, want slug go", p.Categories)
	}
	if len(p.Tags) != 1 || p.Tags[0].Slug != "backend" {
		t.Fatalf("tags = %+v, want slug backend", p.Tags)
	}

	comments, err := st.ApprovedComments(123)
	if err != nil {
		t.Fatalf("load comments: %v", err)
	}
	if len(comments) != 1 || comments[0].ID != 900 {
		t.Fatalf("comments = %+v, want only id 900", comments)
	}
	setting, err := st.GetSetting("site_name")
	if err != nil {
		t.Fatalf("get setting: %v", err)
	}
	if setting != "测试站" {
		t.Fatalf("site_name = %q, want 测试站", setting)
	}
}

func TestExportXMLRoundTrip(t *testing.T) {
	src := newTestStore(t)
	db := src.DB()
	if err := db.Create(&model.User{ID: 9, Username: "writer", DisplayName: "作者", Email: "writer@example.com"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Save(&model.Setting{Key: "site_name", Value: "导出测试站"}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	post := model.Post{ID: 1001, Title: "文章", Slug: "article", Content: "<p>Hello</p>", Excerpt: "摘要", AuthorID: 9, Status: model.StatusPublished, PostType: model.PostTypePost, ContentFormat: model.FormatHTML, CommentStatus: "open"}
	page := model.Post{ID: 1002, Title: "页面", Slug: "about", Content: "<p>About</p>", AuthorID: 9, Status: model.StatusPublished, PostType: model.PostTypePage, ContentFormat: model.FormatHTML, CommentStatus: "closed"}
	cat := model.Category{ID: 11, Name: "Go", Slug: "go"}
	tag := model.Tag{ID: 12, Name: "后端", Slug: "backend"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	if err := db.Create(&tag).Error; err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if err := db.Create(&page).Error; err != nil {
		t.Fatalf("seed page: %v", err)
	}
	if err := db.Model(&post).Association("Categories").Replace([]model.Category{cat}); err != nil {
		t.Fatalf("associate categories: %v", err)
	}
	if err := db.Model(&post).Association("Tags").Replace([]model.Tag{tag}); err != nil {
		t.Fatalf("associate tags: %v", err)
	}
	if err := db.Create(&model.Comment{ID: 501, PostID: 1001, Author: "访客", Email: "guest@example.com", Content: "评论内容", Status: model.CommentApproved}).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	xmlData, stats, err := ExportXML(db, ExportOptions{Posts: true, Pages: true, Comments: true, Settings: true, SiteTitle: "导出测试站", SiteURL: "https://example.com"})
	if err != nil {
		t.Fatalf("export xml: %v", err)
	}
	if stats.Posts != 1 || stats.Pages != 1 || stats.Comments != 1 || stats.Settings != 1 {
		t.Fatalf("unexpected export stats: %+v", stats)
	}

	dst := newTestStore(t)
	dstDB := dst.DB()
	if err := dstDB.Create(&model.User{ID: 77, Username: "target", DisplayName: "目标用户"}).Error; err != nil {
		t.Fatalf("seed target user: %v", err)
	}
	importStats, err := ImportReader(dstDB, strings.NewReader(string(xmlData)), Options{TargetUserID: 77, IncludeDrafts: true})
	if err != nil {
		t.Fatalf("re-import xml: %v", err)
	}
	if importStats.Posts != 1 || importStats.Pages != 1 || importStats.Comments != 1 || importStats.Settings != 1 {
		t.Fatalf("unexpected import stats: %+v", importStats)
	}

	importedPost, err := dst.AdminGetPost(1001)
	if err != nil {
		t.Fatalf("load imported post: %v", err)
	}
	if importedPost.AuthorID != 77 {
		t.Fatalf("author_id = %d, want 77", importedPost.AuthorID)
	}
	if len(importedPost.Categories) != 1 || importedPost.Categories[0].Slug != "go" {
		t.Fatalf("categories = %+v, want go", importedPost.Categories)
	}
	if len(importedPost.Tags) != 1 || importedPost.Tags[0].Slug != "backend" {
		t.Fatalf("tags = %+v, want backend", importedPost.Tags)
	}
	comments, err := dst.ApprovedComments(1001)
	if err != nil {
		t.Fatalf("load imported comments: %v", err)
	}
	if len(comments) != 1 || comments[0].Content != "评论内容" {
		t.Fatalf("comments = %+v, want one imported comment", comments)
	}
	setting, err := dst.GetSetting("site_name")
	if err != nil {
		t.Fatalf("get imported setting: %v", err)
	}
	if setting != "导出测试站" {
		t.Fatalf("site_name = %q, want 导出测试站", setting)
	}
}
