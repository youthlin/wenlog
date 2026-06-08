package store

import (
	"testing"
	"time"

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

	res, err := st.ListPosts(1, 10, "", "")
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

	comments, err := st.ApprovedComments(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].ID != 1 {
		t.Errorf("ApprovedComments returned %d comments, want only approved id=1", len(comments))
	}
}

func TestNextPostID(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	db.Create(&model.Post{ID: 1890, PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: time.Now()})
	id, err := st.NextPostID()
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

	res, err := st.SearchPosts("Go", 1, 10)
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
	if prev := st.PrevPost(mid); prev == nil || prev.ID != 1 {
		t.Errorf("PrevPost = %v, want id 1", prev)
	}
	if next := st.NextPost(mid); next == nil || next.ID != 3 {
		t.Errorf("NextPost = %v, want id 3", next)
	}
	// 边界:最新一篇无下一篇。
	if next := st.NextPost(base.Add(3 * time.Hour)); next != nil {
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

	counts := st.ApprovedCommentCounts([]uint{1, 2})
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
	db.Create(&model.User{ID: 1, Username: "youthlin", Email: "me@example.com"})
	db.Create(&model.Comment{ID: 1, PostID: SayingPageID, Email: "me@example.com", Status: model.CommentApproved, Content: "动态1", CreatedAt: time.Now()})
	db.Create(&model.Comment{ID: 2, PostID: SayingPageID, Email: "guest@x.com", Status: model.CommentApproved, Content: "访客", CreatedAt: time.Now()})

	got := st.SayingComments(5)
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
