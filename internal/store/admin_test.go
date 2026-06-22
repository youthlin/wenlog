package store

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/youthlin/blog/internal/model"
)

func TestUpdateUserProfileAndUserExistsByUsername(t *testing.T) {
	st := newTestStore(t)
	db := st.DB(context.Background())
	if err := db.Create(&model.User{ID: 1, Username: "old", DisplayName: "旧名", Email: "old@example.com"}).Error; err != nil {
		t.Fatalf("seed user1: %v", err)
	}
	if err := db.Create(&model.User{ID: 2, Username: "taken", DisplayName: "占用", Email: "taken@example.com"}).Error; err != nil {
		t.Fatalf("seed user2: %v", err)
	}
	exists, err := st.UserExistsByUsername(context.Background(), "taken", 1)
	if err != nil {
		t.Fatalf("check username: %v", err)
	}
	if !exists {
		t.Fatal("expected username taken to exist for another user")
	}
	exists, err = st.UserExistsByUsername(context.Background(), "taken", 2)
	if err != nil {
		t.Fatalf("check own username: %v", err)
	}
	if exists {
		t.Fatal("expected own username not to be treated as duplicate")
	}
	if err = st.UpdateUserProfile(context.Background(), 1, "newname", "新显示名", "new@example.com", "https://example.com/me"); err != nil {
		t.Fatalf("update profile: %v", err)
	}
	u, err := st.GetUserByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if u.Username != "newname" || u.DisplayName != "新显示名" || u.Email != "new@example.com" || u.Website != "https://example.com/me" {
		t.Fatalf("unexpected updated user: %+v", u)
	}
}

func TestLastAdminCannotBeDemotedOrDeleted(t *testing.T) {
	st := newTestStore(t)
	db := st.DB(context.Background())
	if err := db.Create(&model.User{ID: 1, Username: "admin", Role: model.RoleAdmin}).Error; err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	if err := st.UpdateUserRole(context.Background(), 1, model.RoleAuthor); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("UpdateUserRole error = %v, want ErrLastAdmin", err)
	}
	u, err := st.GetUserByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("load admin: %v", err)
	}
	if u.Role != model.RoleAdmin {
		t.Fatalf("role = %q, want admin", u.Role)
	}

	if err := st.DeleteUser(context.Background(), 1); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("DeleteUser error = %v, want ErrLastAdmin", err)
	}
	var count int64
	if err := db.Model(&model.User{}).Where("id = ?", 1).Count(&count).Error; err != nil {
		t.Fatalf("count admin: %v", err)
	}
	if count != 1 {
		t.Fatalf("admin count = %d, want 1", count)
	}
}

func TestAdminCanBeDemotedOrDeletedWhenAnotherAdminExists(t *testing.T) {
	st := newTestStore(t)
	db := st.DB(context.Background())
	for _, u := range []model.User{
		{ID: 1, Username: "admin1", Role: model.RoleAdmin},
		{ID: 2, Username: "admin2", Role: model.RoleAdmin},
		{ID: 3, Username: "admin3", Role: model.RoleAdmin},
	} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatalf("seed admin: %v", err)
		}
	}

	if err := st.UpdateUserRole(context.Background(), 1, model.RoleAuthor); err != nil {
		t.Fatalf("UpdateUserRole with another admin: %v", err)
	}
	u, err := st.GetUserByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("load demoted user: %v", err)
	}
	if u.Role != model.RoleAuthor {
		t.Fatalf("role = %q, want author", u.Role)
	}

	if err := st.DeleteUser(context.Background(), 2); err != nil {
		t.Fatalf("DeleteUser with another admin: %v", err)
	}
	if _, err := st.GetUserByID(context.Background(), 2); err == nil {
		t.Fatal("expected deleted admin2 to be missing")
	}
}

func TestPendingRegistrationCreatesUserAfterVerification(t *testing.T) {
	st := newTestStore(t)
	expiry := time.Now().Add(time.Hour)
	if err := st.SavePendingRegistration(context.Background(), "newuser", "new@example.com", "token-1", expiry); err != nil {
		t.Fatalf("save pending registration: %v", err)
	}
	pending, err := st.GetPendingRegistrationByToken(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("get pending registration: %v", err)
	}
	if pending.Username != "newuser" || pending.Email != "new@example.com" {
		t.Fatalf("pending registration = %+v", pending)
	}

	u, err := st.CompletePendingRegistration(context.Background(), "token-1", "hashed-password")
	if err != nil {
		t.Fatalf("complete pending registration: %v", err)
	}
	if u.Username != "newuser" || u.Email != "new@example.com" || u.Role != model.RoleSubscriber || u.PasswordHash != "hashed-password" {
		t.Fatalf("created user = %+v", u)
	}
	if _, err := st.GetPendingRegistrationByToken(context.Background(), "token-1"); !errors.Is(err, ErrPendingRegistrationNotFound) {
		t.Fatalf("pending registration after complete error = %v, want ErrPendingRegistrationNotFound", err)
	}
}

func TestPendingRegistrationReplacesDuplicateRequest(t *testing.T) {
	st := newTestStore(t)
	if err := st.SavePendingRegistration(context.Background(), "newuser", "new@example.com", "old-token", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("save old pending registration: %v", err)
	}
	if err := st.SavePendingRegistration(context.Background(), "newuser", "new@example.com", "new-token", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("save new pending registration: %v", err)
	}
	if _, err := st.GetPendingRegistrationByToken(context.Background(), "old-token"); !errors.Is(err, ErrPendingRegistrationNotFound) {
		t.Fatalf("old token error = %v, want ErrPendingRegistrationNotFound", err)
	}
	if _, err := st.GetPendingRegistrationByToken(context.Background(), "new-token"); err != nil {
		t.Fatalf("new token should exist: %v", err)
	}
}

func TestPendingEmailChangeUpdatesUserAfterVerification(t *testing.T) {
	st := newTestStore(t)
	db := st.DB(context.Background())
	if err := db.Create(&model.User{ID: 1, Username: "user", DisplayName: "用户", Email: "old@example.com"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := st.SavePendingEmailChange(context.Background(), 1, "new@example.com", "email-token", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("save pending email change: %v", err)
	}
	u, err := st.CompletePendingEmailChange(context.Background(), 1, "email-token")
	if err != nil {
		t.Fatalf("complete pending email change: %v", err)
	}
	if u.Email != "new@example.com" {
		t.Fatalf("email = %q, want new@example.com", u.Email)
	}
	if _, err := st.CompletePendingEmailChange(context.Background(), 1, "email-token"); !errors.Is(err, ErrPendingEmailChangeNotFound) {
		t.Fatalf("repeat complete error = %v, want ErrPendingEmailChangeNotFound", err)
	}
}

func TestPendingEmailChangeReplacesDuplicateRequest(t *testing.T) {
	st := newTestStore(t)
	if err := st.SavePendingEmailChange(context.Background(), 1, "new@example.com", "old-email-token", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("save old pending email change: %v", err)
	}
	if err := st.SavePendingEmailChange(context.Background(), 1, "new@example.com", "new-email-token", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("save new pending email change: %v", err)
	}
	if _, err := st.CompletePendingEmailChange(context.Background(), 1, "old-email-token"); !errors.Is(err, ErrPendingEmailChangeNotFound) {
		t.Fatalf("old token error = %v, want ErrPendingEmailChangeNotFound", err)
	}
}

func TestAdminListPagesOrdersByMenuOrder(t *testing.T) {
	st := newTestStore(t)
	db := st.DB(context.Background())
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	posts := []model.Post{
		{ID: 1, Title: "文章", PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: now.Add(3 * time.Hour)},
		{ID: 2, Title: "页面 30", PostType: model.PostTypePage, Status: model.StatusPublished, MenuOrder: 30, PublishedAt: now.Add(2 * time.Hour)},
		{ID: 3, Title: "页面 10", PostType: model.PostTypePage, Status: model.StatusPublished, MenuOrder: 10, PublishedAt: now.Add(1 * time.Hour)},
		{ID: 4, Title: "页面 20", PostType: model.PostTypePage, Status: model.StatusPublished, MenuOrder: 20, PublishedAt: now},
	}
	for _, p := range posts {
		if err := db.Create(&p).Error; err != nil {
			t.Fatalf("seed post: %v", err)
		}
	}

	pages, total, err := st.AdminListPosts(context.Background(), model.PostTypePage, 1, 10, 0, 0, "")
	if err != nil {
		t.Fatalf("AdminListPosts pages: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	got := []uint{pages[0].ID, pages[1].ID, pages[2].ID}
	want := []uint{3, 4, 2}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("page order = %v, want %v", got, want)
		}
	}
}

func TestDeleteCategoryMovesPostsToParent(t *testing.T) {
	st := newTestStore(t)
	db := st.DB(context.Background())
	parent := model.Category{ID: 1, Name: "Parent", Slug: "parent"}
	child := model.Category{ID: 2, Name: "Child", Slug: "child", ParentID: parent.ID}
	post := model.Post{ID: 10, Title: "Post", PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: time.Now()}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := db.Create(&child).Error; err != nil {
		t.Fatalf("seed child: %v", err)
	}
	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if err := db.Model(&post).Association("Categories").Replace([]model.Category{child}); err != nil {
		t.Fatalf("seed post category: %v", err)
	}

	if err := st.DeleteCategory(context.Background(), child.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	loaded, err := st.AdminGetPost(context.Background(), post.ID)
	if err != nil {
		t.Fatalf("load post: %v", err)
	}
	if len(loaded.Categories) != 1 || loaded.Categories[0].ID != parent.ID {
		t.Fatalf("categories after delete = %+v, want parent", loaded.Categories)
	}
}

func TestDeleteCategoryMovesPostsToUncategorizedWithoutParent(t *testing.T) {
	st := newTestStore(t)
	db := st.DB(context.Background())
	uncategorized := model.Category{ID: 1, Name: "未分类", Slug: "uncategorized"}
	cat := model.Category{ID: 2, Name: "Go", Slug: "go"}
	post := model.Post{ID: 10, Title: "Post", PostType: model.PostTypePost, Status: model.StatusPublished, PublishedAt: time.Now()}
	for _, c := range []model.Category{uncategorized, cat} {
		if err := db.Create(&c).Error; err != nil {
			t.Fatalf("seed category: %v", err)
		}
	}
	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if err := db.Model(&post).Association("Categories").Replace([]model.Category{cat}); err != nil {
		t.Fatalf("seed post category: %v", err)
	}

	if err := st.DeleteCategory(context.Background(), cat.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	loaded, err := st.AdminGetPost(context.Background(), post.ID)
	if err != nil {
		t.Fatalf("load post: %v", err)
	}
	if len(loaded.Categories) != 1 || loaded.Categories[0].Slug != "uncategorized" {
		t.Fatalf("categories after delete = %+v, want uncategorized", loaded.Categories)
	}
}

func TestDeleteCategoryRejectsUncategorized(t *testing.T) {
	st := newTestStore(t)
	db := st.DB(context.Background())
	uncategorized := model.Category{ID: 1, Name: "未分类", Slug: "uncategorized"}
	if err := db.Create(&uncategorized).Error; err != nil {
		t.Fatalf("seed uncategorized: %v", err)
	}
	if err := st.DeleteCategory(context.Background(), uncategorized.ID); !errors.Is(err, ErrCannotDeleteUncategorized) {
		t.Fatalf("DeleteCategory error = %v, want ErrCannotDeleteUncategorized", err)
	}
}

func TestPageSlugExists(t *testing.T) {
	st := newTestStore(t)
	db := st.DB(context.Background())
	if err := db.Create(&model.Post{ID: 1, PostType: model.PostTypePage, Slug: "about", Status: model.StatusPublished}).Error; err != nil {
		t.Fatalf("seed page: %v", err)
	}
	exists, err := st.PageSlugExists(context.Background(), "about", 0)
	if err != nil {
		t.Fatalf("PageSlugExists: %v", err)
	}
	if !exists {
		t.Fatal("expected about slug to exist")
	}
	exists, err = st.PageSlugExists(context.Background(), "about", 1)
	if err != nil {
		t.Fatalf("PageSlugExists self: %v", err)
	}
	if exists {
		t.Fatal("expected excluded page slug not to count as duplicate")
	}
}

func TestDeleteCommentSoftDeletesChildren(t *testing.T) {
	st := newTestStore(t)
	db := st.DB(context.Background())
	comments := []model.Comment{
		{ID: 1, PostID: 1, ParentID: 0, Status: model.CommentApproved, Content: "parent"},
		{ID: 2, PostID: 1, ParentID: 1, Status: model.CommentApproved, Content: "child"},
		{ID: 3, PostID: 1, ParentID: 2, Status: model.CommentApproved, Content: "grandchild"},
	}
	for _, c := range comments {
		if err := db.Create(&c).Error; err != nil {
			t.Fatalf("seed comment %d: %v", c.ID, err)
		}
	}
	if err := st.DeleteComment(context.Background(), 1); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	var got []model.Comment
	if err := db.Order("id ASC").Find(&got).Error; err != nil {
		t.Fatalf("reload comments: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("comment count = %d, want 3", len(got))
	}
	for _, c := range got {
		if c.Status != model.CommentDeleted {
			t.Fatalf("comment %d status = %q, want %q", c.ID, c.Status, model.CommentDeleted)
		}
	}
}
