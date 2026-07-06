package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/wenlog/internal/i18n"
)

// RevisionsPage 显示文章的修订版本列表。
func (h *Admin) RevisionsPage(c *gin.Context) {
	tr := i18n.Get(c)
	postID, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	p, err := h.st.AdminGetPost(c, postID)
	if err != nil {
		h.notFound(c)
		return
	}
	if !h.canManagePost(c, p) {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	revs, err := h.st.ListRevisions(c, postID)
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, tr.T("修订版本"))
	data["Post"] = p
	data["Revisions"] = revs
	c.HTML(http.StatusOK, "admin_revisions.gohtml", data)
}

// RevisionView 查看单个修订版本的内容。
func (h *Admin) RevisionView(c *gin.Context) {
	tr := i18n.Get(c)
	id, err := parseUintParam(c.Param("revId"))
	if err != nil {
		h.notFound(c)
		return
	}
	rev, err := h.st.GetRevision(c, id)
	if err != nil {
		h.notFound(c)
		return
	}
	p, err := h.st.AdminGetPost(c, rev.PostID)
	if err != nil {
		h.notFound(c)
		return
	}
	if !h.canManagePost(c, p) {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	data := h.base(c, tr.T("查看修订版本"))
	data["Post"] = p
	data["Revision"] = rev
	c.HTML(http.StatusOK, "admin_revision_view.gohtml", data)
}

// RevisionRestore 回滚到指定修订版本。
func (h *Admin) RevisionRestore(c *gin.Context) {
	id, err := parseUintParam(c.Param("revId"))
	if err != nil {
		h.notFound(c)
		return
	}
	rev, err := h.st.GetRevision(c, id)
	if err != nil {
		h.notFound(c)
		return
	}
	p, err := h.st.AdminGetPost(c, rev.PostID)
	if err != nil {
		h.notFound(c)
		return
	}
	if !h.canManagePost(c, p) {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}

	// 回滚前先保存当前版本
	if err := h.st.CreateRevision(c, p); err != nil && h.log != nil {
		h.log.WarnContext(c, "create revision before restore", "post_id", p.ID, "error", err)
	}

	// 用修订版本内容覆盖文章
	p.Title = rev.Title
	p.ContentMD = rev.ContentMD
	p.Content = rev.Content
	p.Excerpt = rev.Excerpt
	if err := h.st.SavePost(c, p); err != nil {
		h.serverError(c, err)
		return
	}

	c.Redirect(http.StatusSeeOther, "/admin/post/"+strconv.FormatUint(uint64(p.ID), 10)+"/revisions")
}
