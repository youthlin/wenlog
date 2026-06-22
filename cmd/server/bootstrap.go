package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	gettext "github.com/youthlin/t"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"
)

var (
	bootstrapWelcomePostTitle = gettext.Mark.T("欢迎来到我的博客")
	bootstrapWelcomeMarkdown  = gettext.Mark.T("欢迎使用这个独立博客程序。它专注于：\n\n- **简单部署**：单二进制 + SQLite，适合个人博客快速上线。\n- **内容优先**：支持 Markdown 写作，也兼容逐步整理已有内容。\n- **可持续维护**：后台可管理文章、页面、评论、资源与模板。\n\n你可以先到后台看看设置、写一篇文章、再把这里改成真正属于你自己的首页开场白。祝写作愉快。")
	bootstrapWelcomeComment   = gettext.Mark.T("欢迎使用这个博客程序，你可以在后台管理评论，快去看看吧。")
	bootstrapAboutPageTitle   = gettext.Mark.T("关于")
	bootstrapAboutMarkdown    = gettext.Mark.T("你好，欢迎来到这里。\n\n我是这个博客的作者，喜欢记录技术、写作、日常想法，也会把一些正在尝试和思考的东西慢慢整理出来。这里不会只放“结论”，也会保留过程、踩坑和一些还没完全想清楚的问题。\n\n如果你恰好读到某篇文章，希望它能给你一点启发；如果你也在做相似的事情，欢迎交流。")
	bootstrapMsgRandomPasswd  = gettext.Mark.T("已生成随机密码: %s\n")
	bootstrapErrPasswdHash    = gettext.Mark.T("密码处理失败, err=%v\n")
	bootstrapMsgResetPasswd   = gettext.Mark.T("已为用户 %s 重置密码\n")
	bootstrapMsgInitialAdmin  = gettext.Mark.T("已自动创建管理员, 用户名: admin 密码: %s\n")
	bootstrapErrUserNotFound  = gettext.Mark.T("用户不存在: %s\n")
	bootstrapMsgAdminList     = gettext.Mark.T("当前管理员列表:\n")
)

var resetPassword = flag.String("reset-password", "",
	"重置用户密码:格式 username[:password],密码不填会自动生成并打印,设置后退出")

// setPasswd 如果有 -reset-password 参数 执行密码重置 并退出。
func setPasswd(st *store.Store) bool {
	t := gettext.Global()
	ctx := context.Background()
	spec := *resetPassword
	if spec == "" { // 没有传该参数
		return false
	}
	username, password, ok := strings.Cut(spec, ":")
	if !ok {
		username = spec
		password = util.GenerateRandomString(8)
		fmt.Printf(t.T(bootstrapMsgRandomPasswd), password)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, t.T(bootstrapErrPasswdHash), err)
		os.Exit(1)
	}
	err = st.SetUserPassword(ctx, username, string(hash))
	if err != nil {
		fmt.Fprintf(os.Stderr, t.T(bootstrapErrUserNotFound), username)
		admins, listErr := st.ListUsersByRole(ctx, model.RoleAdmin)
		if listErr == nil && len(admins) > 0 {
			fmt.Fprint(os.Stderr, t.T(bootstrapMsgAdminList))
			for _, a := range admins {
				fmt.Fprintf(os.Stderr, "  %s (%s)\n", a.Username, a.DisplayName)
			}
		}
		os.Exit(1)
	}
	fmt.Printf(t.T(bootstrapMsgResetPasswd), username)
	return true
}

// ensureInitialAdmin 启动时如果没有用户, 自动创建 admin 用户。
func ensureInitialAdmin(st *store.Store) error {
	ctx := context.Background()
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		// 已有用户 直接返回
		return nil
	}
	password := util.GenerateRandomString(12)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return errors.Wrap(err, "密码加密失败")
	}
	if err := st.CreateAdmin(ctx, "admin", string(hash)); err != nil {
		return err
	}
	fmt.Printf(gettext.T(bootstrapMsgInitialAdmin), password)
	return nil
}

// ensureInitialContent 启动时如果还没有任何内容, 自动插入欢迎文章、示例评论和关于页面。
// i18n.Init 在 main 中先执行, 这里读取当前全局语言, 让首批内容尽量跟随服务器默认语言。
func ensureInitialContent(st *store.Store) error {
	ctx := context.Background()
	total, err := st.CountPosts(ctx)
	if err != nil {
		return err
	}
	if total > 0 {
		return nil
	}
	author, err := st.GetUserByUsername(ctx, "admin")
	if err != nil {
		users, listErr := st.ListUsers(ctx)
		if listErr != nil {
			return listErr
		}
		if len(users) == 0 {
			return errors.Errorf("系统未初始化,数据库中没有任何用户,无法插入初始内容")
		}
		author = &users[0]
	}
	uncategorized := &model.Category{
		Name: gettext.T("未分类"),
		Slug: "uncategorized",
	}
	if err = st.SaveCategory(ctx, uncategorized); err != nil {
		return err
	}
	now := time.Now()
	postID, err := st.NextPostID(ctx)
	if err != nil {
		return err
	}
	welcomeMD := strings.TrimSpace(gettext.T(bootstrapWelcomeMarkdown))
	welcomPost := &model.Post{
		ID:            postID,
		Title:         gettext.T(bootstrapWelcomePostTitle),
		ContentMD:     welcomeMD,
		Content:       render.RenderMarkdown(welcomeMD),
		AuthorID:      author.ID,
		Status:        model.StatusPublished,
		PostType:      model.PostTypePost,
		ContentFormat: model.FormatMarkdown,
		CommentStatus: "open",
		PublishedAt:   now,
		ModifiedAt:    now,
	}
	if err = st.SavePostWithTerms(ctx, welcomPost, []uint{uncategorized.ID}, nil); err != nil {
		return err
	}
	comment := &model.Comment{
		PostID:    welcomPost.ID,
		Author:    "youthlin",
		Email:     "youthlinchen@outlook.com",
		URL:       "https://github.com/youthlin/blog",
		IP:        "127.0.0.1",
		Content:   gettext.T(bootstrapWelcomeComment),
		Status:    model.CommentApproved,
		CreatedAt: now.Add(time.Minute),
		ParentID:  0,
	}
	if err = st.CreateComment(ctx, comment); err != nil {
		return err
	}
	aboutID, err := st.NextPostID(ctx)
	if err != nil {
		return err
	}
	aboutMD := strings.TrimSpace(gettext.T(bootstrapAboutMarkdown))
	aboutPage := &model.Post{
		ID:            aboutID,
		Title:         gettext.T(bootstrapAboutPageTitle),
		Slug:          "about",
		ContentMD:     aboutMD,
		Content:       render.RenderMarkdown(aboutMD),
		AuthorID:      author.ID,
		Status:        model.StatusPublished,
		PostType:      model.PostTypePage,
		ContentFormat: model.FormatMarkdown,
		CommentStatus: "closed",
		MenuOrder:     1,
		PublishedAt:   now,
		ModifiedAt:    now,
	}
	return st.SavePost(ctx, aboutPage)
}
