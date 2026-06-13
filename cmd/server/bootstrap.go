package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gomarkdown/markdown"
	mhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	gettext "github.com/youthlin/t"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/render"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"
)

var (
	bootstrapWelcomePostTitle = gettext.Mark.T("欢迎来到我的博客")
	bootstrapAboutPageTitle   = gettext.Mark.T("关于")
	bootstrapWelcomeComment   = gettext.Mark.T("欢迎使用这个博客程序，你可以在后台管理评论，快去看看吧。")
	bootstrapWelcomeMarkdown  = gettext.Mark.T("欢迎使用这个独立博客程序。它专注于：\n\n- **简单部署**：单二进制 + SQLite，适合个人博客快速上线。\n- **内容优先**：支持 Markdown 写作，也兼容逐步整理已有内容。\n- **可持续维护**：后台可管理文章、页面、评论、资源与模板。\n\n你可以先到后台看看设置、写一篇文章、再把这里改成真正属于你自己的首页开场白。祝写作愉快。")
	bootstrapAboutMarkdown    = gettext.Mark.T("你好，欢迎来到这里。\n\n我是这个博客的作者，喜欢记录技术、写作、日常想法，也会把一些正在尝试和思考的东西慢慢整理出来。这里不会只放“结论”，也会保留过程、踩坑和一些还没完全想清楚的问题。\n\n如果你恰好读到某篇文章，希望它能给你一点启发；如果你也在做相似的事情，欢迎交流。")
	bootstrapMsgRandomPasswd  = gettext.Mark.T("已生成随机密码: %s\n")
	bootstrapErrPasswdHash    = gettext.Mark.T("密码处理失败, err=%v\n")
	bootstrapErrSetPasswd     = gettext.Mark.T("设置密码失败, err=%v\n")
	bootstrapMsgResetPasswd   = gettext.Mark.T("已为用户 %s 重置密码\n")
	bootstrapMsgInitialAdmin  = gettext.Mark.T("已自动创建管理员, 用户名: admin 密码: %s\n")
	bootstrapErrNoAuthor      = gettext.Mark.T("初始化内容失败: 未找到可用作者")
	bootstrapErrUserNotFound  = gettext.Mark.T("用户不存在: %s\n")
	bootstrapMsgAdminList     = gettext.Mark.T("当前管理员列表:\n")
)

var resetPassword = flag.String("reset-password", "",
	"重置用户密码:格式 username[:password],密码不填会自动生成并打印,设置后退出")

// setPasswd 如果有 -reset-password 参数 执行密码重置 并退出。
func setPasswd(st *store.Store) bool {
	t := gettext.Global()
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
	err = st.SetUserPassword(username, string(hash))
	if err != nil {
		fmt.Fprintf(os.Stderr, t.T(bootstrapErrUserNotFound), username)
		admins, listErr := st.ListUsersByRole(model.RoleAdmin)
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
	t := gettext.Global()
	n, err := st.CountUsers()
	if err != nil {
		return err
	}
	if n > 0 {
		// 确保现有 admin 用户拥有 admin 角色(兼容旧数据迁移)
		if err := st.EnsureAdminRole("admin"); err != nil {
			return err
		}
		return nil
	}
	password := util.GenerateRandomString(12)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := st.UpsertUserPassword("admin", "admin", string(hash)); err != nil {
		return err
	}
	// 新创建的 admin 用户需要显式设为 admin 角色
	if err := st.EnsureAdminRole("admin"); err != nil {
		return err
	}
	fmt.Printf(t.T(bootstrapMsgInitialAdmin), password)
	return nil
}

// ensureInitialContent 启动时如果还没有任何内容, 自动插入欢迎文章、示例评论和关于页面。
// i18n.Init 在 main 中先执行, 这里读取当前全局语言, 让首批内容尽量跟随服务器默认语言。
func ensureInitialContent(st *store.Store) error {
	t := gettext.Global()
	total, err := st.CountPosts()
	if err != nil {
		return err
	}
	if total > 0 {
		return nil
	}
	author, err := st.GetUserByUsername("admin")
	if err != nil {
		users, listErr := st.ListUsers()
		if listErr != nil {
			return listErr
		}
		if len(users) == 0 {
			return fmt.Errorf("%s", t.T(bootstrapErrNoAuthor))
		}
		author = &users[0]
	}
	uncategorized := &model.Category{
		Name: t.T("未分类"),
		Slug: "uncategorized",
	}
	if err = st.SaveCategory(uncategorized); err != nil {
		return err
	}
	now := time.Now()
	postID, err := st.NextPostID()
	if err != nil {
		return err
	}
	welcomeMD := strings.TrimSpace(t.T(bootstrapWelcomeMarkdown))
	welcome := &model.Post{
		ID:            postID,
		Title:         t.T(bootstrapWelcomePostTitle),
		ContentMD:     welcomeMD,
		Content:       renderMarkdownForBootstrap(welcomeMD),
		AuthorID:      author.ID,
		Status:        model.StatusPublished,
		PostType:      model.PostTypePost,
		ContentFormat: model.FormatMarkdown,
		CommentStatus: "open",
		PublishedAt:   now,
		ModifiedAt:    now,
	}
	if err = st.SavePostWithTerms(welcome, []uint{uncategorized.ID}, nil); err != nil {
		return err
	}
	comment := &model.Comment{
		PostID:    welcome.ID,
		Author:    "youthlin",
		Email:     "youthlinchen@outlook.com",
		URL:       "https://github.com/youthlin/blog",
		IP:        "127.0.0.1",
		Content:   t.T(bootstrapWelcomeComment),
		Status:    model.CommentApproved,
		CreatedAt: now.Add(time.Minute),
		ParentID:  0,
	}
	if err = st.CreateComment(comment); err != nil {
		return err
	}
	aboutID, err := st.NextPostID()
	if err != nil {
		return err
	}
	aboutMD := strings.TrimSpace(t.T(bootstrapAboutMarkdown))
	about := &model.Post{
		ID:            aboutID,
		Title:         t.T(bootstrapAboutPageTitle),
		Slug:          "about",
		ContentMD:     aboutMD,
		Content:       renderMarkdownForBootstrap(aboutMD),
		AuthorID:      author.ID,
		Status:        model.StatusPublished,
		PostType:      model.PostTypePage,
		ContentFormat: model.FormatMarkdown,
		CommentStatus: "closed",
		MenuOrder:     1,
		PublishedAt:   now,
		ModifiedAt:    now,
	}
	return st.SavePost(about)
}

func renderMarkdownForBootstrap(md string) string {
	p := parser.NewWithExtensions(parser.CommonExtensions | parser.AutoHeadingIDs)
	doc := p.Parse([]byte(md))
	rendererMD := mhtml.NewRenderer(mhtml.RendererOptions{Flags: mhtml.CommonFlags})
	out := string(markdown.Render(doc, rendererMD))
	return render.HighlightCodeBlocks(out)
}
