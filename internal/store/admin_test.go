package store

import (
	"testing"

	"github.com/youthlin/blog/internal/model"
)

func TestUpdateUserProfileAndUserExistsByUsername(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	if err := db.Create(&model.User{ID: 1, Username: "old", DisplayName: "旧名", Email: "old@example.com"}).Error; err != nil {
		t.Fatalf("seed user1: %v", err)
	}
	if err := db.Create(&model.User{ID: 2, Username: "taken", DisplayName: "占用", Email: "taken@example.com"}).Error; err != nil {
		t.Fatalf("seed user2: %v", err)
	}
	exists, err := st.UserExistsByUsername("taken", 1)
	if err != nil {
		t.Fatalf("check username: %v", err)
	}
	if !exists {
		t.Fatal("expected username taken to exist for another user")
	}
	exists, err = st.UserExistsByUsername("taken", 2)
	if err != nil {
		t.Fatalf("check own username: %v", err)
	}
	if exists {
		t.Fatal("expected own username not to be treated as duplicate")
	}
	if err := st.UpdateUserProfile(1, "newname", "新显示名", "new@example.com"); err != nil {
		t.Fatalf("update profile: %v", err)
	}
	u, err := st.GetUserByID(1)
	if err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if u.Username != "newname" || u.DisplayName != "新显示名" || u.Email != "new@example.com" {
		t.Fatalf("unexpected updated user: %+v", u)
	}
}

func TestPageSlugExists(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
	if err := db.Create(&model.Post{ID: 1, PostType: model.PostTypePage, Slug: "about", Status: model.StatusPublished}).Error; err != nil {
		t.Fatalf("seed page: %v", err)
	}
	exists, err := st.PageSlugExists("about", 0)
	if err != nil {
		t.Fatalf("PageSlugExists: %v", err)
	}
	if !exists {
		t.Fatal("expected about slug to exist")
	}
	exists, err = st.PageSlugExists("about", 1)
	if err != nil {
		t.Fatalf("PageSlugExists self: %v", err)
	}
	if exists {
		t.Fatal("expected excluded page slug not to count as duplicate")
	}
}

func TestDeleteCommentSoftDeletesChildren(t *testing.T) {
	st := newTestStore(t)
	db := st.DB()
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
	if err := st.DeleteComment(1); err != nil {
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
