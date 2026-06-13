package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/permalink"
)

// MyComments 前台我的评论列表。
func (h *Public) MyComments(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	page := atoiDefault(c.Query("page"), 1)
	comments, total, err := h.st.ListCommentsByUser(u.ID, page, commentPageSize)
	if err != nil {
		h.serverError(c, err)
		return
	}
	postIDs := make([]uint, 0, len(comments))
	for _, cm := range comments {
		postIDs = append(postIDs, cm.PostID)
	}
	postsByID, err := h.st.AdminPostsByIDs(postIDs)
	if err != nil {
		h.serverError(c, err)
		return
	}
	postTitles := make(map[uint]string, len(postsByID))
	postLinks := make(map[uint]string, len(postsByID))
	for id, post := range postsByID {
		postTitles[id] = post.Title
		if post.PostType == "page" {
			postLinks[id] = permalink.Page(&post)
		} else {
			postLinks[id] = permalink.Post(&post)
		}
	}
	s := h.loadSettings()
	data := h.base(c, tr.T("我的评论"), "", s)
	data["Comments"] = comments
	data["PostTitles"] = postTitles
	data["PostLinks"] = postLinks
	data["Total"] = total
	data["Page"] = page
	data["Pages"] = int((total + int64(commentPageSize) - 1) / int64(commentPageSize))
	c.HTML(http.StatusOK, "my_comments.gohtml", data)
}

// DeleteMyComment 删除自己的评论。
func (h *Public) DeleteMyComment(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	commentID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.st.DeleteCommentByUser(uint(commentID), u.ID); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/my-comments")
}
