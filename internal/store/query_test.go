package store

import (
	"context"
	"testing"
	"time"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/model"
)

// newTestStore 建一个临时文件 SQLite(:memory: 在 glebarez 下连接复用有坑,用临时文件)。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return st
}

func TestListPostsAndPagination(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	now := time.Now()
	for i := 1; i <= 15; i++ {
		p := model.Post{
			ID: uint(i), Title: "post", PostType: model.PostTypePost,
			Status: model.StatusPublished, PublishedAt: now.Add(time.Duration(i) * time.Hour),
		}
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 一篇草稿不应出现。
	db.Create(&model.Post{ID: 99, PostType: model.PostTypePost, Status: model.StatusDraft, PublishedAt: now})

	res, err := st.ListPosts(context.Background(), 1, 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 15 {
		t.Errorf("Total = %d, want 15", res.Total)
	}
	if res.Pages != 2 {
		t.Errorf("Pages = %d, want 2", res.Pages)
	}
	if len(res.Posts) != 10 {
		t.Errorf("page1 len = %d, want 10", len(res.Posts))
	}
	// 倒序:第一篇应是 id=15(发布时间最新)。
	if res.Posts[0].ID != 15 {
		t.Errorf("first post id = %d, want 15", res.Posts[0].ID)
	}
}

func TestApprovedCommentsOnly(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	db.Create(&model.Post{ID: 1, PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: time.Now()})
	db.Create(&model.Comment{ID: 1, PostID: 1, Status: model.CommentApproved, Content: "a", CreatedAt: time.Now()})
	db.Create(&model.Comment{ID: 2, PostID: 1, Status: model.CommentPending, Content: "b", CreatedAt: time.Now()})

	comments, err := st.ApprovedComments(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].ID != 1 {
		t.Errorf("ApprovedComments returned %d comments, want only approved id=1", len(comments))
	}
}

func TestVisibleCommentsIncludesOwnPendingOnly(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	uid := uint(7)
	now := time.Now()
	db.Create(&model.Post{ID: 1, PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: now})
	db.Create(&model.Comment{ID: 1, PostID: 1, Status: model.CommentApproved, Content: "approved", CreatedAt: now.Add(-3 * time.Minute)})
	db.Create(&model.Comment{ID: 2, PostID: 1, UserID: &uid, Status: model.CommentPending, Content: "own pending", CreatedAt: now.Add(-2 * time.Minute)})
	db.Create(&model.Comment{ID: 3, PostID: 1, Status: model.CommentPending, Content: "session pending", CreatedAt: now.Add(-1 * time.Minute)})
	db.Create(&model.Comment{ID: 4, PostID: 1, Status: model.CommentPending, Content: "other pending", CreatedAt: now})

	comments, err := st.VisibleCommentsPageForViewer(context.Background(), 1, 1, 10, uid, []uint{3})
	if err != nil {
		t.Fatal(err)
	}
	if comments.TotalComments != 3 || len(comments.Comments) != 3 {
		t.Fatalf("visible comments count=%d len=%d, want 3", comments.TotalComments, len(comments.Comments))
	}
	got := map[uint]bool{}
	for _, c := range comments.Comments {
		got[c.ID] = true
	}
	for _, id := range []uint{1, 2, 3} {
		if !got[id] {
			t.Fatalf("visible comments missing id=%d: %+v", id, got)
		}
	}
	if got[4] {
		t.Fatalf("other user's pending comment should not be visible: %+v", got)
	}
}

func TestResolveCommentReplyKeepsTwoLevelThread(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	now := time.Now()
	db.Create(&model.Post{ID: 1, PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: now})
	db.Create(&model.Comment{ID: 1, PostID: 1, ParentID: 0, Status: model.CommentApproved, Author: "parent", CreatedAt: now})
	db.Create(&model.Comment{ID: 2, PostID: 1, ParentID: 1, ReplyToID: 1, Status: model.CommentApproved, Author: "child", CreatedAt: now.Add(time.Minute)})

	parentID, replyToID, err := st.ResolveCommentReply(context.Background(), 1, 2, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if parentID != 1 || replyToID != 2 {
		t.Fatalf("ResolveCommentReply parent=%d replyTo=%d, want parent=1 replyTo=2", parentID, replyToID)
	}

	if err := st.CreateComment(context.Background(), &model.Comment{ID: 3, PostID: 1, ParentID: parentID, ReplyToID: replyToID, Status: model.CommentApproved, Author: "reply", CreatedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	comments, err := st.VisibleCommentsPageForViewer(context.Background(), 1, 1, 10, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	var reply model.Comment
	for _, c := range comments.Comments {
		if c.ID == 3 {
			reply = c
		}
	}
	if reply.ParentID != 1 || reply.ReplyToID != 2 || reply.ReplyToAuthor != "child" {
		t.Fatalf("reply = %+v, want parent=1 replyTo=2 replyToAuthor=child", reply)
	}
}

func TestCommentsByIDs(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	db.Create(&model.Comment{ID: 1, PostID: 1, Author: "a"})
	db.Create(&model.Comment{ID: 2, PostID: 1, Author: "b"})
	db.Create(&model.Comment{ID: 3, PostID: 1, Author: "c"})

	comments, err := st.CommentsByIDs(context.Background(), []uint{1, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 {
		t.Fatalf("CommentsByIDs len=%d, want 2", len(comments))
	}
	got := map[uint]bool{}
	for _, comment := range comments {
		got[comment.ID] = true
	}
	if !got[1] || !got[3] || got[2] {
		t.Fatalf("CommentsByIDs returned IDs=%v", got)
	}

	comments, err = st.CommentsByIDs(context.Background(), nil)
	if err != nil || len(comments) != 0 {
		t.Fatalf("CommentsByIDs nil=(%v,%v), want empty nil error", comments, err)
	}
}

func TestNextPostID(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	db.Create(&model.Post{ID: 1890, PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: time.Now()})
	id, err := st.NextPostID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id != 1891 {
		t.Errorf("NextPostID = %d, want 1891", id)
	}
}

func TestSearchPosts(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	now := time.Now()
	db.Create(&model.Post{ID: 1, Title: "学习 Go 语言", Content: "hello", PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: now})
	db.Create(&model.Post{ID: 2, Title: "随笔", Content: "今天写了 Golang 代码", PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: now})
	db.Create(&model.Post{ID: 3, Title: "无关", Content: "java", PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: now})
	// 草稿命中也不应返回。
	db.Create(&model.Post{ID: 4, Title: "Go 草稿", PostType: model.PostTypePost, Status: model.StatusDraft, PublishedAt: now})

	res, err := st.SearchPosts(context.Background(), "Go", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Errorf("search Go total = %d, want 2", res.Total)
	}
}

func TestPrevNextPost(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		db.Create(&model.Post{ID: uint(i), Title: "p", PostType: model.PostTypePost,
			Status: model.StatusPublished, PublishedAt: base.Add(time.Duration(i) * time.Hour)})
	}
	mid := base.Add(2 * time.Hour)
	if prev := st.PrevPost(context.Background(), mid); prev == nil || prev.ID != 1 {
		t.Errorf("PrevPost = %v, want id 1", prev)
	}
	if next := st.NextPost(context.Background(), mid); next == nil || next.ID != 3 {
		t.Errorf("NextPost = %v, want id 3", next)
	}
	// 边界:最新一篇无下一篇。
	if next := st.NextPost(context.Background(), base.Add(3*time.Hour)); next != nil {
		t.Errorf("NextPost(newest) = %v, want nil", next)
	}
}

func TestApprovedCommentCounts(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	db.Create(&model.Post{ID: 1, PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: time.Now()})
	db.Create(&model.Post{ID: 2, PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: time.Now()})
	db.Create(&model.Comment{ID: 1, PostID: 1, Status: model.CommentApproved, CreatedAt: time.Now()})
	db.Create(&model.Comment{ID: 2, PostID: 1, Status: model.CommentApproved, CreatedAt: time.Now()})
	db.Create(&model.Comment{ID: 3, PostID: 1, Status: model.CommentPending, CreatedAt: time.Now()})

	counts := st.ApprovedCommentCounts(context.Background(), []uint{1, 2})
	if counts[1] != 2 {
		t.Errorf("count post1 = %d, want 2", counts[1])
	}
	if counts[2] != 0 {
		t.Errorf("count post2 = %d, want 0", counts[2])
	}
}

func TestSayingComments(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	sayingPostID := uint(789)
	db.Create(&model.User{ID: 1, Username: "youthlin", Email: "me@example.com"})
	db.Create(&model.Comment{ID: 1, PostID: sayingPostID, Email: "me@example.com", Status: model.CommentApproved, Content: "动态1", CreatedAt: time.Now()})
	db.Create(&model.Comment{ID: 2, PostID: sayingPostID, Email: "guest@x.com", Status: model.CommentApproved, Content: "访客", CreatedAt: time.Now()})
	db.Create(&model.Comment{ID: 3, PostID: uint(consts.SettingsSayingPageIDDefault), Email: "me@example.com", Status: model.CommentApproved, Content: "旧配置", CreatedAt: time.Now()})

	got := st.SayingComments(context.Background(), sayingPostID, 5)
	if len(got) != 1 || got[0].ID != 1 {
		t.Errorf("SayingComments returned %d, want only blogger's id=1", len(got))
	}
}

func TestSlugifyTag(t *testing.T) {
	cases := map[string]string{
		"Hello World": "hello-world",
		"Go语言":        "go语言",
		"  C++  ":     "c",
	}
	for in, want := range cases {
		if got := slugifyTag(in); got != want {
			t.Errorf("slugifyTag(%q) = %q, want %q", in, got, want)
		}
	}
}
