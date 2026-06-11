package handler

import (
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
)

// 评论限制常量。
const (
	commentMaxLen   = 5000
	commentMinLen   = 2
	rateWindowSec   = 60 // 限频窗口:60 秒
	rateMaxInWindow = 3  // 窗口内同 IP 最多 3 条
)

// commentReq 是评论提交表单。
type commentReq struct {
	PostID   uint   `form:"post_id"`
	ParentID uint   `form:"parent_id"`
	Author   string `form:"author"`
	Email    string `form:"email"`
	URL      string `form:"url"`
	Content  string `form:"content"`
	Notify   string `form:"notify"`
	Website  string `form:"website"` // 蜜罐字段:正常用户不填
}

// SubmitComment 处理 POST /comment, 校验 + 防 spam + 存为 pending。
// 支持 Ajax(返回 JSON)与普通表单(重定向)两种方式。
func (h *Public) SubmitComment(c *gin.Context) {
	tr := i18n.Get(c)
	var req commentReq
	if err := c.ShouldBind(&req); err != nil {
		h.commentResp(c, false, tr.T("提交内容格式有误。"), req.PostID)
		return
	}

	// 1. 蜜罐:website 字段被填说明是机器人。
	if strings.TrimSpace(req.Website) != "" {
		h.commentResp(c, false, tr.T("提交失败。"), req.PostID)
		return
	}

	// 2. 基础校验。
	req.Author = strings.TrimSpace(req.Author)
	req.Content = strings.TrimSpace(req.Content)
	req.Email = strings.TrimSpace(req.Email)
	loggedInUser := h.commentUser(c)
	if loggedInUser != nil {
		req.Author = strings.TrimSpace(loggedInUser.DisplayName)
		if req.Author == "" {
			req.Author = strings.TrimSpace(loggedInUser.Username)
		}
		req.Email = strings.TrimSpace(loggedInUser.Email)
		req.URL = ""
	}
	if req.Author == "" || req.Content == "" {
		h.commentResp(c, false, tr.T("昵称和内容不能为空。"), req.PostID)
		return
	}
	if n := utf8.RuneCountInString(req.Content); n < commentMinLen || n > commentMaxLen {
		h.commentResp(c, false, tr.T("评论长度不合适。"), req.PostID)
		return
	}
	if loggedInUser == nil && req.Email == "" {
		h.commentResp(c, false, tr.T("邮箱不能为空。"), req.PostID)
		return
	}
	if req.Email != "" {
		if _, err := mail.ParseAddress(req.Email); err != nil {
			h.commentResp(c, false, tr.T("邮箱格式不正确。"), req.PostID)
			return
		}
	}

	// 3. 目标文章/页面必须存在、已发布、评论开放。
	target, err := h.st.GetPostByID(req.PostID)
	if err != nil {
		// 也可能是页面(saying/guestbook),用 PostMeta 兜底校验。
		meta, err2 := h.st.PostMeta(req.PostID)
		if err2 != nil || meta.Status != model.StatusPublished {
			h.commentResp(c, false, tr.T("目标文章不存在。"), req.PostID)
			return
		}
		target = meta
	}
	if target.CommentStatus == "closed" {
		h.commentResp(c, false, tr.T("该文章评论已关闭。"), req.PostID)
		return
	}

	// 4. 限频:同 IP 60 秒内最多 3 条。
	ip := c.ClientIP()
	since := time.Now().Add(-rateWindowSec * time.Second).Unix()
	if cnt, _ := h.st.RecentCommentCountByIP(ip, since); cnt >= rateMaxInWindow {
		h.commentResp(c, false, tr.T("评论太频繁,请稍后再试。"), req.PostID)
		return
	}

	// 5. 存为待审。
	cm := &model.Comment{
		PostID:        req.PostID,
		ParentID:      req.ParentID,
		Author:        req.Author,
		Email:         req.Email,
		URL:           strings.TrimSpace(req.URL),
		IP:            ip,
		Content:       req.Content,
		Status:        model.CommentPending,
		NotifyOnReply: req.Notify != "",
		CreatedAt:     time.Now(),
	}
	if err := h.st.CreateComment(cm); err != nil {
		h.serverError(c, err)
		return
	}
	h.commentResp(c, true, tr.T("评论已提交,等待审核后显示。"), req.PostID)
}

// commentResp 根据请求类型返回 JSON 或重定向。
func (h *Public) commentResp(c *gin.Context, ok bool, msg string, postID uint) {
	if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
		status := http.StatusOK
		if !ok {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"ok": ok, "message": msg})
		return
	}
	// 非 Ajax:重定向回文章页。
	if p, err := h.st.PostMeta(postID); err == nil {
		c.Redirect(http.StatusSeeOther, postRedirectURL(p))
		return
	}
	c.Redirect(http.StatusSeeOther, "/")
}

// postRedirectURL 返回文章/页面的链接(用于非 Ajax 提交后跳回)。
func postRedirectURL(p *model.Post) string {
	if p.PostType == model.PostTypePage {
		return permalink.Page(p) + "#comments"
	}
	return permalink.Post(p) + "#comments"
}

func (h *Public) commentUser(c *gin.Context) *model.User {
	s := sessions.Default(c)
	v := s.Get(middleware.SessionUserKey)
	uid, ok := v.(uint)
	if !ok || uid == 0 {
		return nil
	}
	u, err := h.st.GetUserByID(uid)
	if err != nil {
		return nil
	}
	return u
}
