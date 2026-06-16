package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
)

// Dashboard 后台首页。
func (h *Admin) Dashboard(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	data := h.base(c, tr.T("欢迎"))
	switch u.Role {
	case model.RoleAdmin:
		data["Stats"] = h.st.DashboardStats(c)
	case model.RoleAuthor:
		data["AuthorStats"] = h.st.AuthorDashboardStats(c, u.ID)
	default:
		data["ReaderStats"] = h.st.ReaderDashboardStats(c.Request.Context(), u.ID)
	}
	data["RecentPosts"] = h.st.RecentPosts(c.Request.Context(), 5)
	comments := h.st.RecentComments(c, 5)
	data["RecentComments"] = comments
	if len(comments) > 0 {
		postIDs := make([]uint, 0, len(comments))
		for _, comment := range comments {
			postIDs = append(postIDs, comment.PostID)
		}
		postsByID, err := h.st.AdminPostsByIDs(c, postIDs)
		if err == nil {
			titles := make(map[uint]string, len(postsByID))
			urls := make(map[uint]string, len(postsByID))
			for id, p := range postsByID {
				titles[id] = p.Title
				urls[id] = permalink.Post(&p)
			}
			data["CommentPostTitles"] = titles
			data["CommentPostURLs"] = urls
		}
	}
	c.HTML(http.StatusOK, "admin_dashboard.gohtml", data)
}
