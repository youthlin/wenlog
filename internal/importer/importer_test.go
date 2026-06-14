package importer

import (
	"strings"
	"testing"
	"time"

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
    <wp:author><wp:author_id>5</wp:author_id><wp:author_login>someone</wp:author_login><wp:author_email>someone@example.com</wp:author_email><wp:author_display_name>Someone</wp:author_display_name></wp:author>
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
        <wp:comment_user_id>5</wp:comment_user_id>
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
	wantPublishedAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.Local)
	if !p.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("published_at = %s, want %s", p.PublishedAt, wantPublishedAt)
	}
	wantModifiedAt := time.Date(2024, 1, 3, 3, 4, 5, 0, time.Local)
	if !p.ModifiedAt.Equal(wantModifiedAt) {
		t.Fatalf("modified_at = %s, want %s", p.ModifiedAt, wantModifiedAt)
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
	if comments[0].UserID == nil || *comments[0].UserID != 7 {
		t.Fatalf("comment user_id = %v, want 7", comments[0].UserID)
	}
	wantCommentCreatedAt := time.Date(2024, 1, 4, 3, 4, 5, 0, time.Local)
	if !comments[0].CreatedAt.Equal(wantCommentCreatedAt) {
		t.Fatalf("comment created_at = %s, want %s", comments[0].CreatedAt, wantCommentCreatedAt)
	}
	setting, err := st.GetSetting("site_name")
	if err != nil {
		t.Fatalf("get setting: %v", err)
	}
	if setting != "测试站" {
		t.Fatalf("site_name = %q, want 测试站", setting)
	}
}

func TestImportReaderUsesGMTDatesWhenLocalDatesAreMissing(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	if err := db.Create(&model.User{ID: 7, Username: "admin", DisplayName: "管理员"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	xml := `<?xml version="1.0" encoding="UTF-8" ?>
<rss version="2.0"
	xmlns:content="http://purl.org/rss/1.0/modules/content/"
	xmlns:dc="http://purl.org/dc/elements/1.1/"
	xmlns:wp="http://wordpress.org/export/1.2/">
  <channel>
    <item>
      <title>GMT 时间文章</title>
      <pubDate>Tue, 02 Jan 2024 03:04:05 +0000</pubDate>
      <dc:creator>someone</dc:creator>
      <content:encoded><![CDATA[<p>Hello</p>]]></content:encoded>
      <wp:post_id>321</wp:post_id>
      <wp:post_date>0000-00-00 00:00:00</wp:post_date>
      <wp:post_date_gmt>2024-01-02 03:04:05</wp:post_date_gmt>
      <wp:post_modified>0000-00-00 00:00:00</wp:post_modified>
      <wp:post_modified_gmt>2024-01-03 03:04:05</wp:post_modified_gmt>
      <wp:status>publish</wp:status>
      <wp:post_type>post</wp:post_type>
      <wp:comment>
        <wp:comment_id>901</wp:comment_id>
        <wp:comment_author>Alice</wp:comment_author>
        <wp:comment_date>0000-00-00 00:00:00</wp:comment_date>
        <wp:comment_date_gmt>2024-01-04 03:04:05</wp:comment_date_gmt>
        <wp:comment_content><![CDATA[GMT 评论]]></wp:comment_content>
        <wp:comment_approved>1</wp:comment_approved>
      </wp:comment>
    </item>
  </channel>
</rss>`
	if _, err := ImportReader(db, strings.NewReader(xml), Options{TargetUserID: 7, IncludeDrafts: true}); err != nil {
		t.Fatalf("import xml: %v", err)
	}
	p, err := st.AdminGetPost(321)
	if err != nil {
		t.Fatalf("load imported post: %v", err)
	}
	wantPublishedAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if !p.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("published_at = %s, want %s", p.PublishedAt, wantPublishedAt)
	}
	wantModifiedAt := time.Date(2024, 1, 3, 3, 4, 5, 0, time.UTC)
	if !p.ModifiedAt.Equal(wantModifiedAt) {
		t.Fatalf("modified_at = %s, want %s", p.ModifiedAt, wantModifiedAt)
	}
	comments, err := st.ApprovedComments(321)
	if err != nil {
		t.Fatalf("load comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comments = %+v, want one", comments)
	}
	wantCommentCreatedAt := time.Date(2024, 1, 4, 3, 4, 5, 0, time.UTC)
	if !comments[0].CreatedAt.Equal(wantCommentCreatedAt) {
		t.Fatalf("comment created_at = %s, want %s", comments[0].CreatedAt, wantCommentCreatedAt)
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

func TestExportXMLRoundTripMapsMultipleAuthorsAndCommentUsers(t *testing.T) {
	src := newTestStore(t)
	db := src.DB()
	users := []model.User{
		{ID: 9, Username: "writer", DisplayName: "作者", Email: "writer@example.com"},
		{ID: 10, Username: "editor", DisplayName: "编辑", Email: "editor@example.com"},
	}
	for _, u := range users {
		if err := db.Create(&u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	posts := []model.Post{
		{ID: 2001, Title: "作者文章", Slug: "writer-post", Content: "<p>writer</p>", AuthorID: 9, Status: model.StatusPublished, PostType: model.PostTypePost, ContentFormat: model.FormatHTML, CommentStatus: "open"},
		{ID: 2002, Title: "编辑页面", Slug: "editor-page", Content: "<p>editor</p>", AuthorID: 10, Status: model.StatusPublished, PostType: model.PostTypePage, ContentFormat: model.FormatHTML, CommentStatus: "open"},
	}
	for _, p := range posts {
		if err := db.Create(&p).Error; err != nil {
			t.Fatalf("seed post: %v", err)
		}
	}
	writerID := uint(9)
	editorID := uint(10)
	comments := []model.Comment{
		{ID: 601, PostID: 2001, UserID: &writerID, Author: "作者", Email: "writer@example.com", Content: "作者自己的评论", Status: model.CommentApproved},
		{ID: 602, PostID: 2002, UserID: &editorID, Author: "编辑", Email: "editor@example.com", Content: "编辑自己的评论", Status: model.CommentApproved},
	}
	for _, c := range comments {
		if err := db.Create(&c).Error; err != nil {
			t.Fatalf("seed comment: %v", err)
		}
	}

	xmlData, stats, err := ExportXML(db, ExportOptions{Posts: true, Pages: true, Comments: true, SiteTitle: "多作者站", SiteURL: "https://example.com"})
	if err != nil {
		t.Fatalf("export xml: %v", err)
	}
	if stats.Posts != 1 || stats.Pages != 1 || stats.Comments != 2 {
		t.Fatalf("unexpected export stats: %+v", stats)
	}
	xmlText := string(xmlData)
	for _, want := range []string{"<wp:author_id>9</wp:author_id>", "<wp:author_id>10</wp:author_id>", "<wp:comment_user_id>9</wp:comment_user_id>", "<wp:comment_user_id>10</wp:comment_user_id>"} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("export xml missing %q:\n%s", want, xmlText)
		}
	}

	dst := newTestStore(t)
	dstDB := dst.DB()
	for _, u := range []model.User{
		{ID: 77, Username: "target-writer", DisplayName: "目标作者"},
		{ID: 88, Username: "target-editor", DisplayName: "目标编辑"},
	} {
		if err := dstDB.Create(&u).Error; err != nil {
			t.Fatalf("seed target user: %v", err)
		}
	}
	importStats, err := ImportReader(dstDB, strings.NewReader(xmlText), Options{
		TargetUserID:  77,
		IncludeDrafts: true,
		AuthorMapping: map[string]uint{"writer": 77, "editor": 88},
	})
	if err != nil {
		t.Fatalf("re-import xml: %v", err)
	}
	if importStats.Posts != 1 || importStats.Pages != 1 || importStats.Comments != 2 {
		t.Fatalf("unexpected import stats: %+v", importStats)
	}
	importedWriterPost, err := dst.AdminGetPost(2001)
	if err != nil {
		t.Fatalf("load writer post: %v", err)
	}
	if importedWriterPost.AuthorID != 77 {
		t.Fatalf("writer post author_id = %d, want 77", importedWriterPost.AuthorID)
	}
	importedEditorPage, err := dst.AdminGetPost(2002)
	if err != nil {
		t.Fatalf("load editor page: %v", err)
	}
	if importedEditorPage.AuthorID != 88 {
		t.Fatalf("editor page author_id = %d, want 88", importedEditorPage.AuthorID)
	}
	var importedComments []model.Comment
	if err := dstDB.Order("id ASC").Find(&importedComments).Error; err != nil {
		t.Fatalf("load imported comments: %v", err)
	}
	if len(importedComments) != 2 {
		t.Fatalf("imported comments = %+v, want two comments", importedComments)
	}
	if importedComments[0].UserID == nil || *importedComments[0].UserID != 77 {
		t.Fatalf("writer comment user_id = %v, want 77", importedComments[0].UserID)
	}
	if importedComments[1].UserID == nil || *importedComments[1].UserID != 88 {
		t.Fatalf("editor comment user_id = %v, want 88", importedComments[1].UserID)
	}
}
