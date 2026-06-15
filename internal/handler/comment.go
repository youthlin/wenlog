package handler

import (
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/email"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/store"
)

// 评论限制常量。
const (
	commentMaxLen   = 5000
	commentMinLen   = 2
	rateWindowSec   = 60 // 限频窗口:60 秒
	rateMaxInWindow = 3  // 窗口内同 IP 最多 3 条

	pendingCommentIDsSessionKey = "pending_comment_ids"
	maxPendingCommentIDs        = 100

	commenterAuthorSessionKey = "commenter_author"
	commenterEmailSessionKey  = "commenter_email"
	commenterURLSessionKey    = "commenter_url"
)

// commentReq 是评论提交表单。
type commentReq struct {
	PostID    uint   `form:"post_id"`
	ParentID  uint   `form:"parent_id"`
	ReplyToID uint   `form:"reply_to_id"`
	Author    string `form:"author"`
	Email     string `form:"email"`
	URL       string `form:"url"`
	Content   string `form:"content"`
	Notify    string `form:"notify"`
	Website   string `form:"website"` // 蜜罐字段:正常用户不填
}

// SubmitComment 处理 POST /comment, 校验 + 防 spam + 按身份决定审核状态。
// 支持 Ajax(返回 JSON)与普通表单(重定向)两种方式。
func (h *Public) SubmitComment(c *gin.Context) {
	tr := i18n.Get(c)
	var req commentReq
	if err := c.ShouldBind(&req); err != nil {
		h.commentResp(c, false, tr.T("提交内容格式有误。"), req.PostID)
		return
	}

	loggedInUser := h.commentUser(c)
	if !h.validateCommentInput(c, tr, &req, loggedInUser) {
		return
	}

	target, ok := h.validateCommentTarget(c, tr, req.PostID)
	if !ok {
		return
	}

	parentID, replyToID, err := h.st.ResolveCommentReply(req.PostID, req.ReplyToID, currentCommentUserID(loggedInUser), pendingCommentIDs(c))
	if err != nil {
		h.commentResp(c, false, tr.T("目标评论不存在。"), req.PostID)
		return
	}

	if !h.checkCommentRateLimit(c, tr, req.PostID) {
		return
	}

	// 存储评论:管理员或本文作者免审,其他评论仍进入待审。
	ip := c.ClientIP()
	cm := &model.Comment{
		PostID:        req.PostID,
		ParentID:      parentID,
		ReplyToID:     replyToID,
		Author:        req.Author,
		Email:         req.Email,
		URL:           req.URL,
		IP:            ip,
		Content:       req.Content,
		Status:        commentStatusForUser(loggedInUser, target),
		NotifyOnReply: req.Notify != "" && h.mailEnabled(),
		CreatedAt:     time.Now(),
	}
	if loggedInUser != nil {
		uid := loggedInUser.ID
		cm.UserID = &uid
	}
	if err := h.st.CreateComment(cm); err != nil {
		h.serverError(c, err)
		return
	}
	msg := tr.T("评论已提交,等待审核后显示。")
	if cm.Status == model.CommentApproved {
		msg = tr.T("评论已发布。")
	} else {
		rememberPendingComment(c, cm.ID)
	}
	if loggedInUser == nil {
		rememberCommenter(c, req)
	}
	if cm.Status == model.CommentApproved {
		h.notifyCommentReply(c, cm)
	}
	commentPage := h.st.VisibleCommentPageForViewerID(cm.ID, commentPageSize, currentCommentUserID(loggedInUser), pendingCommentIDs(c))
	h.commentResp(c, true, msg, req.PostID, gin.H{"comment_page": commentPage})
}

// validateCommentInput 校验评论输入:蜜罐、CSRF、必填项、格式。
func (h *Public) validateCommentInput(c *gin.Context, tr *gettext.Translations, req *commentReq, loggedInUser *model.User) bool {
	// 1. 蜜罐:website 字段被填说明是机器人。
	if strings.TrimSpace(req.Website) != "" {
		h.commentResp(c, false, tr.T("提交失败。"), req.PostID)
		return false
	}

	// 2. 基础校验。
	req.Author = strings.TrimSpace(req.Author)
	req.Content = strings.TrimSpace(req.Content)
	req.Email = strings.TrimSpace(req.Email)
	req.URL = strings.TrimSpace(req.URL)
	if loggedInUser != nil {
		if !middleware.VerifyCSRFToken(c) {
			h.commentResp(c, false, tr.T("提交失败。"), req.PostID)
			return false
		}
		req.Author = strings.TrimSpace(loggedInUser.DisplayName)
		if req.Author == "" {
			req.Author = strings.TrimSpace(loggedInUser.Username)
		}
		req.Email = strings.TrimSpace(loggedInUser.Email)
		req.URL = ""
	}
	if req.Author == "" || req.Content == "" {
		h.commentResp(c, false, tr.T("昵称和内容不能为空。"), req.PostID)
		return false
	}
	if n := utf8.RuneCountInString(req.Content); n < commentMinLen || n > commentMaxLen {
		h.commentResp(c, false, tr.T("评论长度不合适。"), req.PostID)
		return false
	}
	if loggedInUser == nil && req.Email == "" {
		h.commentResp(c, false, tr.T("邮箱不能为空。"), req.PostID)
		return false
	}
	if req.Email != "" {
		if _, err := mail.ParseAddress(req.Email); err != nil {
			h.commentResp(c, false, tr.T("邮箱格式不正确。"), req.PostID)
			return false
		}
	}
	if req.URL != "" {
		u, err := url.Parse(req.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			h.commentResp(c, false, tr.T("网站地址格式不正确。"), req.PostID)
			return false
		}
	}
	return true
}

// validateCommentTarget 校验目标文章/页面存在、已发布、评论开放。
func (h *Public) validateCommentTarget(c *gin.Context, tr *gettext.Translations, postID uint) (*model.Post, bool) {
	target, err := h.st.GetPostByID(postID)
	if err != nil {
		meta, err2 := h.st.PostMeta(postID)
		if err2 != nil || meta.Status != model.StatusPublished {
			h.commentResp(c, false, tr.T("目标文章不存在。"), postID)
			return nil, false
		}
		target = meta
	}
	if target.CommentStatus == "closed" {
		h.commentResp(c, false, tr.T("该文章评论已关闭。"), postID)
		return nil, false
	}
	return target, true
}

// checkCommentRateLimit 检查同 IP 评论频率限制。
func (h *Public) checkCommentRateLimit(c *gin.Context, tr *gettext.Translations, postID uint) bool {
	ip := c.ClientIP()
	since := time.Now().Add(-rateWindowSec * time.Second).Unix()
	cnt, err := h.st.RecentCommentCountByIP(ip, since)
	if err != nil {
		h.log.Error("rate limit check failed", "error", err, "ip", ip)
		h.commentResp(c, false, tr.T("评论太频繁,请稍后再试。"), postID)
		return false
	}
	if cnt >= rateMaxInWindow {
		h.commentResp(c, false, tr.T("评论太频繁,请稍后再试。"), postID)
		return false
	}
	return true
}

func (h *Public) notifyCommentReply(c *gin.Context, reply *model.Comment) {
	tr := i18n.Get(c)
	notifyApprovedCommentReply(h.st, h.log, h.loadSMTPConfig(), siteURLFromRequest(h.st, c), siteNameFromStore(h.st), reply, tr)
}

func notifyApprovedCommentReply(st *store.Store, log *slog.Logger, smtpCfg email.Config, siteURL string, siteName string, reply *model.Comment, tr *gettext.Translations) {
	if st == nil || reply == nil || reply.Status != model.CommentApproved || reply.ReplyToID == 0 || !smtpCfg.Configured() {
		return
	}
	target, err := st.GetCommentByID(reply.ReplyToID)
	if err != nil || target == nil || !target.NotifyOnReply || target.Email == "" {
		return
	}
	if target.Status == model.CommentSpam || target.Status == model.CommentDeleted {
		return
	}
	if sameEmail(target.Email, reply.Email) {
		return
	}
	if _, err := mail.ParseAddress(target.Email); err != nil {
		return
	}
	post, err := st.PostMeta(reply.PostID)
	if err != nil {
		return
	}
	commentURL := strings.TrimRight(siteURL, "/") + commentAnchorURL(post, reply.ID)
	subject, body := commentReplyMail(tr, siteName, post.Title, target.Author, reply.Author, reply.Content, commentURL)
	go func() {
		if err := smtpCfg.Send(target.Email, subject, body); err != nil && log != nil {
			log.Error("send comment reply email", "error", err, "to", target.Email, "comment_id", reply.ID)
		}
	}()
}

func siteNameFromStore(st *store.Store) string {
	settings, _ := st.GetSettings(consts.SettingsSiteName)
	if name := strings.TrimSpace(settings[consts.SettingsSiteName]); name != "" {
		return name
	}
	return consts.SettingsSiteNameDefault
}

func sameEmail(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func commentAnchorURL(p *model.Post, commentID uint) string {
	if p.PostType == model.PostTypePage {
		return permalink.Page(p) + "#comment-" + strconv.FormatUint(uint64(commentID), 10)
	}
	return permalink.Post(p) + "#comment-" + strconv.FormatUint(uint64(commentID), 10)
}

func commentReplyMail(tr *gettext.Translations, siteName, postTitle, targetAuthor, replyAuthor, replyContent, commentURL string) (string, string) {
	subject := tr.T("[%s] 你的评论有新回复", siteName)
	body := tr.T("您好 %s，\n\n%s 回复了你在《%s》下的评论：\n\n%s\n\n查看回复：\n%s\n\n如果你不想再收到通知，可联系站点管理员关闭该评论的回复通知。\n",
		targetAuthor, replyAuthor, postTitle, replyContent, commentURL)
	return subject, body
}

func commentStatusForUser(u *model.User, target *model.Post) string {
	if u == nil || target == nil {
		return model.CommentPending
	}
	if u.Role == model.RoleAdmin || (target.AuthorID != 0 && target.AuthorID == u.ID) {
		return model.CommentApproved
	}
	return model.CommentPending
}

func currentCommentUserID(u *model.User) uint {
	if u == nil {
		return 0
	}
	return u.ID
}

func pendingCommentIDs(c *gin.Context) []uint {
	s := sessions.Default(c)
	raw, _ := s.Get(pendingCommentIDsSessionKey).(string)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]uint, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
		if err == nil && n > 0 {
			ids = append(ids, uint(n))
		}
	}
	return ids
}

func rememberPendingComment(c *gin.Context, id uint) {
	if id == 0 {
		return
	}
	ids := pendingCommentIDs(c)
	ids = append(ids, id)
	if len(ids) > maxPendingCommentIDs {
		ids = ids[len(ids)-maxPendingCommentIDs:]
	}
	parts := make([]string, 0, len(ids))
	for _, pendingID := range ids {
		parts = append(parts, strconv.FormatUint(uint64(pendingID), 10))
	}
	s := sessions.Default(c)
	s.Set(pendingCommentIDsSessionKey, strings.Join(parts, ","))
	_ = s.Save()
}

func rememberedCommenter(c *gin.Context) gin.H {
	s := sessions.Default(c)
	return gin.H{
		"Author": sessionString(s, commenterAuthorSessionKey),
		"Email":  sessionString(s, commenterEmailSessionKey),
		"URL":    sessionString(s, commenterURLSessionKey),
	}
}

func rememberCommenter(c *gin.Context, req commentReq) {
	s := sessions.Default(c)
	s.Set(commenterAuthorSessionKey, strings.TrimSpace(req.Author))
	s.Set(commenterEmailSessionKey, strings.TrimSpace(req.Email))
	s.Set(commenterURLSessionKey, strings.TrimSpace(req.URL))
	_ = s.Save()
}

func sessionString(s sessions.Session, key string) string {
	if s == nil {
		return ""
	}
	v, _ := s.Get(key).(string)
	return v
}

// commentResp 根据请求类型返回 JSON 或重定向。
func (h *Public) commentResp(c *gin.Context, ok bool, msg string, postID uint, extra ...gin.H) {
	if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
		status := http.StatusOK
		if !ok {
			status = http.StatusBadRequest
		}
		payload := gin.H{"ok": ok, "message": msg}
		for _, fields := range extra {
			for k, v := range fields {
				payload[k] = v
			}
		}
		c.JSON(status, payload)
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
