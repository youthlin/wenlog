package handler

import (
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
)

// --- 评论 ---

// ListComments 后台评论列表。支持 mine=1 参数过滤当前用户的评论。
func (h *Admin) ListComments(c *gin.Context) {
	tr := i18n.Get(c)
	status := c.DefaultQuery("status", model.CommentApproved)
	switch status {
	case model.CommentApproved, model.CommentPending, model.CommentSpam:
	default:
		status = model.CommentApproved
	}
	postID := uint(atoiDefault(c.Query("post_id"), 0))
	page := atoiDefault(c.Query("page"), 1)
	mine := c.Query("mine") == "1" || strings.HasPrefix(c.Request.URL.Path, "/admin/my-comments")

	var comments []model.Comment
	var total int64
	var err error

	if mine {
		u := h.currentUser(c)
		if u == nil {
			h.notFound(c)
			return
		}
		comments, total, err = h.st.ListCommentsByUser(c, u.ID, page, adminPageSize)
	} else {
		var authorID uint
		if h.currentUserRole(c) == model.RoleAuthor {
			authorID = currentUserID(c)
		}
		comments, total, err = h.st.AdminListCommentsForAuthor(c, status, postID, page, adminPageSize, authorID)
	}
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, tr.T("评论管理"))
	if mine {
		data["Title"] = tr.T("我的评论")
	}
	postIDs := make([]uint, 0, len(comments))
	for _, comment := range comments {
		postIDs = append(postIDs, comment.PostID)
	}
	postsByID, err := h.st.AdminPostsByIDs(c, postIDs)
	if err != nil {
		h.serverError(c, err)
		return
	}
	commentPostTitles := make(map[uint]string, len(postsByID))
	commentPostLinks := make(map[uint]string, len(postsByID))
	for id, post := range postsByID {
		commentPostTitles[id] = post.Title
		commentPostLinks[id] = permalink.Post(&post)
	}
	data["Comments"] = comments
	data["CommentPostTitles"] = commentPostTitles
	data["CommentPostLinks"] = commentPostLinks
	data["Total"] = total
	data["FilterStatus"] = status
	data["FilterPostID"] = postID
	data["Mine"] = mine
	if postID > 0 {
		if post, ok := postsByID[postID]; ok {
			data["FilterPostTitle"] = post.Title
		}
	}
	data["Counts"] = h.st.AdminCommentCounts(c)
	data["Page"] = page
	pages := int((total + int64(adminPageSize) - 1) / int64(adminPageSize))
	data["Pages"] = pages
	if page > 1 {
		data["PrevPageURL"] = adminCommentPageURL(mine, status, postID, page-1)
	}
	if page < pages {
		data["NextPageURL"] = adminCommentPageURL(mine, status, postID, page+1)
	}
	c.HTML(http.StatusOK, "admin_comments.gohtml", data)
}

func adminCommentPageURL(mine bool, status string, postID uint, page int) string {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	if mine {
		return "/admin/my-comments?" + q.Encode()
	}
	q.Set("status", status)
	if postID > 0 {
		q.Set("post_id", strconv.FormatUint(uint64(postID), 10))
	}
	return "/admin/comments?" + q.Encode()
}

// EditComment 修改评论内容和作者元信息。
func (h *Admin) EditComment(c *gin.Context) {
	id, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	if !h.canManageComment(c, id) {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	fields, ok := parseCommentEditFields(c, "/admin/comments")
	if !ok {
		return
	}
	if err := h.st.UpdateCommentFields(c, id, fields); err != nil {
		h.serverError(c, err)
		return
	}
	safeRedirect(c, "/admin/comments")
}

// EditMyComment 修改当前登录用户自己的评论。
func (h *Admin) EditMyComment(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	id, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	comment, err := h.st.GetCommentByID(c, id)
	if err != nil || comment == nil || comment.UserID == nil || *comment.UserID != u.ID {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	fields, ok := parseCommentEditFields(c, "/admin/my-comments")
	if !ok {
		return
	}
	if err := h.st.UpdateCommentFields(c, id, fields); err != nil {
		h.serverError(c, err)
		return
	}
	safeRedirect(c, "/admin/my-comments")
}

func parseCommentEditFields(c *gin.Context, fallback string) (map[string]any, bool) {
	fields := map[string]any{}
	if _, ok := c.GetPostForm("content"); ok {
		content := strings.TrimSpace(c.PostForm("content"))
		if content == "" {
			safeRedirect(c, fallback)
			return nil, false
		}
		fields["content"] = content
	}
	if _, ok := c.GetPostForm("author"); ok {
		author := strings.TrimSpace(c.PostForm("author"))
		if author == "" {
			safeRedirect(c, fallback)
			return nil, false
		}
		fields["author"] = author
	}
	if _, ok := c.GetPostForm("email"); ok {
		email := strings.TrimSpace(c.PostForm("email"))
		if email == "" {
			safeRedirect(c, fallback)
			return nil, false
		}
		if _, err := mail.ParseAddress(email); err != nil {
			safeRedirect(c, fallback)
			return nil, false
		}
		fields["email"] = email
	}
	if _, ok := c.GetPostForm("url"); ok {
		urlValue := strings.TrimSpace(c.PostForm("url"))
		if urlValue != "" && !strings.Contains(urlValue, "://") {
			urlValue = "https://" + urlValue
		}
		if urlValue != "" {
			if _, err := url.ParseRequestURI(urlValue); err != nil {
				safeRedirect(c, fallback)
				return nil, false
			}
		}
		fields["url"] = urlValue
	}
	if len(fields) == 0 {
		safeRedirect(c, fallback)
		return nil, false
	}
	return fields, true
}

// BatchComments 批量操作评论(approve/pending/spam/delete)。
func (h *Admin) BatchComments(c *gin.Context) {
	action := c.PostForm("action")
	idStrs := c.PostFormArray("ids")
	var ids []uint
	for _, s := range idStrs {
		if n, err := strconv.ParseUint(s, 10, 64); err == nil {
			ids = append(ids, uint(n))
		}
	}
	var notifyCandidates []model.Comment
	if action == "approve" {
		comments, _ := h.st.CommentsByIDs(c, h.filterManageableCommentIDs(c, ids))
		for _, comment := range comments {
			if comment.Status != model.CommentApproved {
				notifyCandidates = append(notifyCandidates, comment)
			}
		}
	}
	ids = h.filterManageableCommentIDs(c, ids)
	if len(ids) == 0 {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	var err error
	switch action {
	case "approve":
		err = h.st.BatchSetCommentStatus(c, ids, model.CommentApproved)
	case "pending":
		err = h.st.BatchSetCommentStatus(c, ids, model.CommentPending)
	case "spam":
		err = h.st.BatchSetCommentStatus(c, ids, model.CommentSpam)
	case "delete":
		err = h.st.BatchDeleteComments(c, ids)
	default:
		safeRedirect(c, "/admin/comments")
		return
	}
	if err != nil {
		h.serverError(c, err)
		return
	}
	if action == "approve" {
		h.notifyApprovedCommentReplies(c, notifyCandidates)
	}
	safeRedirect(c, "/admin/comments")
}

// ModerateComment 审核评论(approve/spam/delete)。
func (h *Admin) ModerateComment(c *gin.Context) {
	id, parseErr := parseUintParam(c.Param("id"))
	if parseErr != nil {
		h.notFound(c)
		return
	}
	if !h.canManageComment(c, id) {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	action := c.Param("action")
	var notifyCandidate *model.Comment
	if action == "approve" {
		if comment, e := h.st.GetCommentByID(c, id); e == nil && comment.Status != model.CommentApproved {
			notifyCandidate = comment
		}
	}
	var err error
	switch action {
	case "approve":
		err = h.st.SetCommentStatus(c, id, model.CommentApproved)
	case "pending":
		err = h.st.SetCommentStatus(c, id, model.CommentPending)
	case "spam":
		err = h.st.SetCommentStatus(c, id, model.CommentSpam)
	case "delete":
		err = h.st.DeleteComment(c, id)
	default:
		h.notFound(c)
		return
	}
	if err != nil {
		h.serverError(c, err)
		return
	}
	if action == "approve" && notifyCandidate != nil {
		h.notifyApprovedCommentReplies(c, []model.Comment{*notifyCandidate})
	}
	safeRedirect(c, "/admin/comments")
}

func (h *Admin) notifyApprovedCommentReplies(c *gin.Context, comments []model.Comment) {
	if len(comments) == 0 {
		return
	}
	smtpCfg := smtpConfigFromStore(c, h.st)
	if !smtpCfg.Configured() {
		return
	}
	siteURL := siteURLFromRequest(h.st, c)
	siteName := siteNameFromStore(c, h.st)
	tr := i18n.Get(c)
	for i := range comments {
		comments[i].Status = model.CommentApproved
		notifyApprovedCommentReply(c, h.st, h.log, smtpCfg, siteURL, siteName, &comments[i], tr)
	}
}

// DeleteMyComment 删除当前用户的评论。
func (h *Admin) DeleteMyComment(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	commentID, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	if err := h.st.DeleteCommentByUser(c, commentID, u.ID); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/my-comments")
}
