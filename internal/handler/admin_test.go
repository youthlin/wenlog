package handler

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"
	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/middleware"
	"github.com/youthlin/wenlog/internal/model"
	"github.com/youthlin/wenlog/internal/plugin"
	renderx "github.com/youthlin/wenlog/internal/render"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/util"
)

func TestAllowDebugSQL(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{name: "select", sql: "SELECT * FROM posts", want: true},
		{name: "explain", sql: "EXPLAIN QUERY PLAN SELECT * FROM posts", want: true},
		{name: "pragma denied", sql: "PRAGMA journal_mode=WAL", want: false},
		{name: "show denied", sql: "SHOW TABLES", want: false},
	}
	for _, tt := range tests {
		if got := allowDebugSQL(tt.sql); got != tt.want {
			t.Fatalf("%s: allowDebugSQL(%q) = %v, want %v", tt.name, tt.sql, got, tt.want)
		}
	}
}

func TestValidatePageSlug(t *testing.T) {
	tests := []struct {
		name string
		slug string
		ok   bool
	}{
		{name: "normal", slug: "about", ok: true},
		{name: "reserved", slug: "search", ok: false},
		{name: "post permalink style", slug: "2024123.html", ok: false},
		{name: "multi segment", slug: "foo/bar", ok: false},
		{name: "blank", slug: " ", ok: false},
	}
	for _, tt := range tests {
		err := validatePageSlug(tt.slug)
		if (err == nil) != tt.ok {
			t.Fatalf("%s: validatePageSlug(%q) err=%v, want ok=%v", tt.name, tt.slug, err, tt.ok)
		}
	}
}

func TestNormalizeShortcodeRulesOptionRejectsUnsafeAttrNames(t *testing.T) {
	opt := plugin.OptionDecl{
		ID:   "shortcode_rules",
		Type: "shortcode_rules",
		Options: []plugin.SelectOpt{
			{Value: "src", Label: "src"},
			{Value: "class", Label: "class"},
			{Value: "title", Label: "title"},
		},
	}
	_, err := normalizeShortcodeRulesOption(opt, `[{"name":"cube","tag":"iframe","attrs":[{"name":"onload","pattern":".*"}]}]`)
	if err == nil || !strings.Contains(err.Error(), "不在允许名单中") {
		t.Fatalf("normalizeShortcodeRulesOption unsafe attr err = %v, want whitelist error", err)
	}
}

func TestNormalizeShortcodeRulesOptionRejectsAttrsOutsideFixedWhitelist(t *testing.T) {
	opt := plugin.OptionDecl{
		ID:   "shortcode_rules",
		Type: "shortcode_rules",
		Options: []plugin.SelectOpt{
			{Value: "data-custom", Label: "data-custom"},
		},
	}
	_, err := normalizeShortcodeRulesOption(opt, `[{"name":"demo","tag":"div","attrs":[{"name":"data-custom","pattern":".*"}]}]`)
	if err == nil || !strings.Contains(err.Error(), "不在允许名单中") {
		t.Fatalf("normalizeShortcodeRulesOption non-standard attr err = %v, want whitelist error", err)
	}
}

func TestNormalizeShortcodeRulesOptionNormalizesValidConfig(t *testing.T) {
	opt := plugin.OptionDecl{
		ID:   "shortcode_rules",
		Type: "shortcode_rules",
		Options: []plugin.SelectOpt{
			{Value: "src", Label: "src"},
			{Value: "class", Label: "class"},
			{Value: "height", Label: "height"},
		},
	}
	got, err := normalizeShortcodeRulesOption(opt, `[
		{"name":"cube","tag":"IFRAME","attrs":[
			{"name":"SRC","pattern":"^https://youthlin\\.com/demo/cube/index\\.html(?:\\?.*)?$"},
			{"name":"class","pattern":"^[A-Za-z0-9 _-]{0,100}$"},
			{"name":"height","pattern":"^[1-9][0-9]{1,3}$"}
		]}
	]`)
	if err != nil {
		t.Fatalf("normalizeShortcodeRulesOption: %v", err)
	}
	var rules []shortcodeRuleOption
	if err := json.Unmarshal([]byte(got), &rules); err != nil {
		t.Fatalf("normalized JSON invalid: %v", err)
	}
	if len(rules) != 1 || rules[0].Name != "cube" || rules[0].Tag != "iframe" {
		t.Fatalf("normalized rules = %+v", rules)
	}
	if rules[0].Attrs[0].Name != "src" {
		t.Fatalf("attr name should be normalized to lowercase, got %+v", rules[0].Attrs[0])
	}
}

func TestNormalizeShortcodeRulesOptionNormalizesEmptyListToDefault(t *testing.T) {
	opt := plugin.OptionDecl{ID: "shortcode_rules", Type: "shortcode_rules"}
	for _, raw := range []string{"[]", "null"} {
		got, err := normalizeShortcodeRulesOption(opt, raw)
		if err != nil {
			t.Fatalf("normalize %s: %v", raw, err)
		}
		if got != "" {
			t.Fatalf("normalize %s = %q, want empty default", raw, got)
		}
	}
}

func TestPostCommentEnhanceShortcodeDefaultConfigIsValid(t *testing.T) {
	p, err := plugin.LoadPlugin(filepath.Join("..", "..", "web", "plugins", "post-comment-enhance"))
	if err != nil {
		t.Fatal(err)
	}
	var opt plugin.OptionDecl
	for _, candidate := range p.Options {
		if candidate.ID == "shortcode_rules" {
			opt = candidate
			break
		}
	}
	if opt.ID == "" {
		t.Fatal("shortcode_rules option not found")
	}
	got, err := normalizeShortcodeRulesOption(opt, opt.Default)
	if err != nil {
		t.Fatalf("normalize default shortcode rules: %v", err)
	}
	if got != "" {
		t.Fatalf("default shortcode rules should be empty, got %s", got)
	}
}

func TestSavePluginSettingsValidationFailureDoesNotPartiallyPersist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	pm, err := plugin.NewManager(filepath.Join("..", "..", "web", "plugins"), st)
	if err != nil {
		t.Fatalf("new plugin manager: %v", err)
	}
	const pluginID = "post-comment-enhance"
	initial := map[string]string{
		plugin.OptionKey(pluginID, "enable_post_smilies"): "false",
		plugin.OptionKey(pluginID, "post_footer_html"):    "<p>old footer</p>",
	}
	if err := st.UpdateSettings(context.Background(), initial, nil); err != nil {
		t.Fatalf("seed plugin settings: %v", err)
	}

	h := &Admin{st: st, pluginManager: pm}
	r := gin.New()
	r.POST("/admin/plugin/:id/settings", h.SavePluginSettings)
	form := url.Values{}
	form.Set("option_enabled", "true")
	form.Set("option_enable_post_smilies", "true")
	form.Set("option_enable_admin_smiley_panel", "true")
	form.Set("option_post_footer_html", "<p>new footer</p>")
	form.Set("option_shortcode_rules", `{"invalid":true}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/plugin/"+pluginID+"/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Fatalf("SavePluginSettings status=%d location=%q", w.Code, w.Header().Get("Location"))
	}
	for key, want := range initial {
		got, err := st.GetSetting(context.Background(), key)
		if err != nil {
			t.Fatalf("get setting %q: %v", key, err)
		}
		if got != want {
			t.Fatalf("setting %q=%q after validation failure, want %q", key, got, want)
		}
	}
}

func TestPreviewUsesUnsavedFieldsAndExistingPostContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	author := model.User{Username: "writer", DisplayName: "Writer", Role: model.RoleAuthor}
	if err := st.DB(context.Background()).Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}
	otherAuthor := model.User{Username: "other", DisplayName: "Other", Role: model.RoleAuthor}
	if err := st.DB(context.Background()).Create(&otherAuthor).Error; err != nil {
		t.Fatalf("create other author: %v", err)
	}
	originalPublishedAt := time.Date(2025, 1, 2, 3, 4, 0, 0, time.Local)
	post := model.Post{
		ID: 42, Title: "Stored title", Slug: "stored-title", ContentMD: "stored body",
		Content: "<p>stored body</p>", ContentFormat: model.FormatMarkdown, AuthorID: author.ID,
		Status: model.StatusPublished, PostType: model.PostTypePost, PublishedAt: originalPublishedAt,
	}
	if err := st.DB(context.Background()).Create(&post).Error; err != nil {
		t.Fatalf("create post: %v", err)
	}

	hooks := hook.NewRegistry()
	var got hook.PostView
	hooks.AddFilter(hook.FilterPostFooterHTML, func(value any, args ...any) any {
		got, _ = args[0].(hook.PostView)
		return value
	}, hook.Source{Type: hook.SourceCore, ID: "test"})
	renderer, err := renderx.New(fstest.MapFS{
		"empty.gohtml": {Data: []byte(`{{define "empty"}}{{end}}`)},
	})
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	renderer.ConfigureTemplateRuntime(renderx.TemplateProviders{Hooks: hooks})
	h := &Admin{st: st, renderer: renderer}

	r := gin.New()
	r.Use(sessions.Sessions("preview_test", cookie.NewStore([]byte("preview-test-secret"))))
	r.POST("/admin/preview", func(c *gin.Context) {
		current := &author
		if c.GetHeader("X-Test-User") == "other" {
			current = &otherAuthor
		}
		s := sessions.Default(c)
		s.Set(middleware.SessionUserKey, current.ID)
		s.Set(middleware.SessionRoleKey, current.Role)
		c.Set(ctxKeyCurrentUser, current)
	}, h.Preview)

	form := url.Values{}
	form.Set("id", "42")
	form.Set("title", " Preview title ")
	form.Set("slug", "Preview Slug")
	form.Set("content_md", "preview **body**")
	form.Set("excerpt", "Preview excerpt")
	form.Set("post_type", model.PostTypePost)
	form.Set("status", model.StatusDraft)
	form.Set("comment_status", "closed")
	form.Set("published_at", "2026-08-23T10:30")
	req := httptest.NewRequest(http.MethodPost, "/admin/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Preview status=%d body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "preview <strong>body</strong>") {
		t.Fatalf("Preview body=%q, want rendered unsaved Markdown", w.Body.String())
	}
	wantPublishedAt := time.Date(2026, 8, 23, 10, 30, 0, 0, time.Local)
	if got.ID != post.ID || got.Title != "Preview title" || got.Slug != "preview-slug" || got.Excerpt != "Preview excerpt" || got.Status != model.StatusDraft || got.PostType != model.PostTypePost {
		t.Fatalf("post.footer_html preview payload = %+v", got)
	}
	if got.AuthorID != author.ID || got.Author.ID != author.ID || got.Author.Username != author.Username || got.Author.DisplayName != author.DisplayName {
		t.Fatalf("post.footer_html preview author = %+v, author_id=%d", got.Author, got.AuthorID)
	}
	if !got.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("post.footer_html preview published_at=%s, want %s", got.PublishedAt, wantPublishedAt)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-User", "other")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Preview another author's post status=%d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestReleaseTagFromURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "github release tag", raw: "https://github.com/youthlin/wenlog/releases/tag/v1.2.3", want: "v1.2.3"},
		{name: "escaped tag", raw: "https://github.com/youthlin/wenlog/releases/tag/v1.2.3%2Bbuild", want: "v1.2.3+build"},
	}
	for _, tt := range tests {
		u, err := url.Parse(tt.raw)
		if err != nil {
			t.Fatalf("parse url: %v", err)
		}
		got, err := releaseTagFromURL(u)
		if err != nil {
			t.Fatalf("%s: releaseTagFromURL err=%v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: releaseTagFromURL()=%q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestToPluginViewIncludesLifecycle(t *testing.T) {
	p := &plugin.Plugin{
		ID:   "demo",
		Name: "Demo",
		Lifecycle: plugin.LifecycleDecl{
			Activate:  true,
			Uninstall: true,
		},
	}
	view := toPluginView(gettext.NewTranslations(), p, false, "")
	got := strings.Join(view.Lifecycle, ",")
	if got != "Activate,Uninstall" {
		t.Fatalf("lifecycle names = %q, want Activate,Uninstall", got)
	}
}

func TestReleaseTagFromURLRejectsLatestURL(t *testing.T) {
	u, err := url.Parse("https://github.com/youthlin/wenlog/releases/latest")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if _, err := releaseTagFromURL(u); err == nil {
		t.Fatal("releaseTagFromURL() err=nil, want error")
	}
}

func TestLatestReleaseFromGoProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/github.com/youthlin/wenlog/@latest" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Version":"v1.2.3","Time":"2026-07-06T00:00:00Z"}`))
	}))
	defer srv.Close()

	release, err := latestReleaseFromGoProxy(context.Background(), srv.URL+"/github.com/youthlin/wenlog/@latest")
	if err != nil {
		t.Fatalf("latestReleaseFromGoProxy err=%v", err)
	}
	if release.TagName != "v1.2.3" || release.HTMLURL != "https://github.com/youthlin/wenlog/releases/tag/v1.2.3" {
		t.Fatalf("release=%+v", release)
	}
}

func TestLooksLikeGoRunTempExecutable(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/tmp/go-build1775786169/b001/exe/server", want: true},
		{path: "/var/tmp/go-build123/b002/exe/wenlog", want: true},
		{path: "/usr/local/bin/wenlog", want: false},
		{path: "/tmp/wenlog", want: false},
	}
	for _, tt := range tests {
		if got := looksLikeGoRunTempExecutable(tt.path); got != tt.want {
			t.Fatalf("looksLikeGoRunTempExecutable(%q)=%v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestUpdateDownloadSourcesPreferGiteeThenGitHub(t *testing.T) {
	sources := updateDownloadSources("v1.2.3", "wenlog-v1.2.3-linux-amd64.tar.gz", "")
	if len(sources) != 2 {
		t.Fatalf("len(sources)=%d, want 2", len(sources))
	}
	if sources[0].Name != "Gitee" || sources[0].ArchiveURL != "https://gitee.com/youthlin/wenlog/releases/download/v1.2.3/wenlog-v1.2.3-linux-amd64.tar.gz" {
		t.Fatalf("first source=%+v", sources[0])
	}
	if sources[1].Name != "GitHub" || sources[1].ArchiveURL != "https://github.com/youthlin/wenlog/releases/download/v1.2.3/wenlog-v1.2.3-linux-amd64.tar.gz" {
		t.Fatalf("second source=%+v", sources[1])
	}
	if sources[0].ChecksumURL != sources[0].ArchiveURL+".sha256" || sources[1].ChecksumURL != sources[1].ArchiveURL+".sha256" {
		t.Fatalf("sources=%+v", sources)
	}
}

func TestUpdateDownloadSourcesUseMirrorOnly(t *testing.T) {
	sources := updateDownloadSources("v1.2.3", "wenlog-v1.2.3-linux-amd64.tar.gz", "https://mirror.example.com/?url={url}")
	if len(sources) != 1 {
		t.Fatalf("len(sources)=%d, want 1", len(sources))
	}
	if sources[0].Name != "下载镜像" {
		t.Fatalf("source=%+v", sources[0])
	}
	if !strings.HasPrefix(sources[0].ArchiveURL, "https://mirror.example.com/?url=") {
		t.Fatalf("archive url=%s", sources[0].ArchiveURL)
	}
}

func TestBeginAppUpdateGuardsConcurrentRun(t *testing.T) {
	adm := &Admin{}
	if !adm.beginAppUpdate() {
		t.Fatal("first beginAppUpdate should start update")
	}
	if !adm.appUpdateInProgress() {
		t.Fatal("appUpdateInProgress should be true after begin")
	}
	if adm.beginAppUpdate() {
		t.Fatal("second beginAppUpdate should reject concurrent update")
	}
	adm.finishAppUpdate()
	if adm.appUpdateInProgress() {
		t.Fatal("appUpdateInProgress should be false after finish")
	}
	if !adm.beginAppUpdate() {
		t.Fatal("beginAppUpdate should start again after finish")
	}
	adm.finishAppUpdate()
}

func TestReadUpdateLogTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wenlog-update.log")
	if err := os.WriteFile(path, []byte("first line\nsecond line\nthird line\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if got := readUpdateLogTail(path, 1024); got != "first line\nsecond line\nthird line" {
		t.Fatalf("full tail=%q", got)
	}
	if got := readUpdateLogTail(path, 24); got != "second line\nthird line" {
		t.Fatalf("partial tail=%q", got)
	}
	if got := readUpdateLogTail(filepath.Join(t.TempDir(), "missing.log"), 1024); got != "" {
		t.Fatalf("missing tail=%q", got)
	}
}

func TestReleaseDirFromFS(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "assets")
	src := fstest.MapFS{
		"style.css":                &fstest.MapFile{Data: []byte("body{}")},
		"nested/app.js":            &fstest.MapFile{Data: []byte("console.log(1)")},
		"nested/deeper/theme.json": &fstest.MapFile{Data: []byte(`{"dark":true}`)},
	}
	if err := releaseDirFromFS(src, targetDir); err != nil {
		t.Fatalf("release dir from fs: %v", err)
	}
	for path, file := range src {
		got, err := os.ReadFile(filepath.Join(targetDir, path))
		if err != nil {
			t.Fatalf("read released file %q: %v", path, err)
		}
		if string(got) != string(file.Data) {
			t.Fatalf("released file %q mismatch: got %q want %q", path, got, file.Data)
		}
	}
	if !util.PathExists(targetDir) {
		t.Fatal("target dir should exist after release")
	}
	if util.PathExists(filepath.Join(targetDir, "missing.txt")) {
		t.Fatal("missing file should not exist")
	}
	if _, err := fs.Stat(os.DirFS(targetDir), "nested/deeper/theme.json"); err != nil {
		t.Fatalf("stat nested released file: %v", err)
	}
}

func TestLocalEditableResourceHelpers(t *testing.T) {
	if got := editableKind("template"); got != editableResourceTemplate {
		t.Fatalf("editableKind(template) = %q", got)
	}
	if got := editableResourceRoot(editableResourceTemplate); got != filepath.Join("web", "templates") {
		t.Fatalf("template root = %q", got)
	}
	if !isEditableLocalFilePath(editableResourceTemplate, "admin_base.gohtml") {
		t.Fatal("template gohtml should be editable")
	}
	if isEditableLocalFilePath(editableResourceTemplate, "admin_base.html") {
		t.Fatal("template html should not be editable")
	}
	if !isEditableLocalFilePath(editableResourceAsset, "admin.css") || !isEditableLocalFilePath(editableResourceAsset, "icons/site.svg") {
		t.Fatal("asset css/svg should be editable")
	}
	if isEditableLocalFilePath(editableResourceAsset, "logo.png") {
		t.Fatal("binary asset should not be editable")
	}
	if !isEditableLocalFilePath(editableResourceI18n, "en_US.po") || !isEditableLocalFilePath(editableResourceI18n, "messages.pot") {
		t.Fatal("i18n po/pot should be editable")
	}
	if isEditableLocalFilePath(editableResourceI18n, "compiled.mo") {
		t.Fatal("i18n mo should not be editable for app resources")
	}
}

func TestNormalizeTermSlug(t *testing.T) {
	tests := map[string]string{
		" Go 教程 ":              "go-教程",
		"Hello, World!":        "hello-world",
		"中文 标签":                "中文-标签",
		"《细说\"Hello, World\"》": "细说hello-world",
	}
	for in, want := range tests {
		if got := normalizeTermSlug(in); got != want {
			t.Fatalf("normalizeTermSlug(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestNormalizeTaxonomySlug(t *testing.T) {
	tests := map[string]string{
		" Go 教程 ":              "go-%e6%95%99%e7%a8%8b",
		"Hello, World!":        "hello-world",
		"中文 标签":                "%e4%b8%ad%e6%96%87-%e6%a0%87%e7%ad%be",
		"《细说\"Hello, World\"》": "%e7%bb%86%e8%af%b4hello-world",
		"%e4%b8%ad%e6%96%87":   "%e4%b8%ad%e6%96%87",
	}
	for in, want := range tests {
		if got := normalizeTaxonomySlug(in); got != want {
			t.Fatalf("normalizeTaxonomySlug(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestDefaultCategorySelectionPrefersUncategorized(t *testing.T) {
	cats := []model.Category{
		{ID: 2, Name: "Go", Slug: "go"},
		{ID: 3, Name: "未分类", Slug: "uncategorized"},
	}
	selected := defaultCategorySelection(cats)
	if !selected[3] || selected[2] {
		t.Fatalf("defaultCategorySelection=%v, want only uncategorized selected", selected)
	}
}

func TestHasSelectedCategoryRequiresExistingCategory(t *testing.T) {
	cats := []model.Category{{ID: 2, Name: "Go", Slug: "go"}}
	if !hasSelectedCategory([]uint{2}, cats) {
		t.Fatal("existing selected category should be valid")
	}
	if hasSelectedCategory([]uint{9}, cats) {
		t.Fatal("unknown selected category should be invalid")
	}
}

func TestCommentStatusForUser(t *testing.T) {
	target := &model.Post{AuthorID: 2}
	tests := []struct {
		name string
		user *model.User
		want string
	}{
		{name: "anonymous", user: nil, want: model.CommentPending},
		{name: "subscriber", user: &model.User{ID: 3, Role: model.RoleSubscriber}, want: model.CommentPending},
		{name: "admin", user: &model.User{ID: 3, Role: model.RoleAdmin}, want: model.CommentApproved},
		{name: "post author", user: &model.User{ID: 2, Role: model.RoleAuthor}, want: model.CommentApproved},
	}
	for _, tt := range tests {
		if got := commentStatusForUser(tt.user, target); got != tt.want {
			t.Fatalf("%s: commentStatusForUser()=%q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestCanEditOwnUsername(t *testing.T) {
	tests := []struct {
		name string
		user *model.User
		want bool
	}{
		{name: "nil", user: nil, want: false},
		{name: "admin", user: &model.User{Role: model.RoleAdmin}, want: true},
		{name: "author", user: &model.User{Role: model.RoleAuthor}, want: false},
		{name: "subscriber", user: &model.User{Role: model.RoleSubscriber}, want: false},
	}
	for _, tt := range tests {
		if got := canEditOwnUsername(tt.user); got != tt.want {
			t.Fatalf("%s: canEditOwnUsername()=%v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestValidOptionalURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{url: "", want: true},
		{url: "https://example.com", want: true},
		{url: "http://example.com/about", want: true},
		{url: "example.com", want: false},
		{url: "mailto:me@example.com", want: false},
		{url: "https://", want: false},
	}
	for _, tt := range tests {
		if got := validOptionalURL(tt.url); got != tt.want {
			t.Fatalf("validOptionalURL(%q)=%v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestRememberCommenter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("test_session", cookie.NewStore([]byte("secret"))))
	r.GET("/set", func(c *gin.Context) {
		rememberCommenter(c, commentReq{
			Author: " Alice ",
			Email:  " alice@example.com ",
			URL:    " https://example.com ",
		})
		c.Status(http.StatusNoContent)
	})
	r.GET("/get", func(c *gin.Context) {
		got := rememberedCommenter(c)
		if got["Author"] != "Alice" || got["Email"] != "alice@example.com" || got["URL"] != "https://example.com" {
			t.Fatalf("rememberedCommenter()=%v", got)
		}
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/set", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("set status=%d", w.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/get", nil)
	for _, cookie := range w.Result().Cookies() {
		req.AddCookie(cookie)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("get status=%d", w.Code)
	}
}

func TestCommentReplyMailHelpers(t *testing.T) {
	if !sameEmail(" Alice@Example.com ", "alice@example.COM") {
		t.Fatal("sameEmail should normalize spaces and case")
	}
	postURL := commentAnchorURL(&model.Post{ID: 42, PostType: model.PostTypePost, PublishedAt: nowForTest()}, 9)
	if !strings.HasSuffix(postURL, "#comment-9") {
		t.Fatalf("post comment anchor=%q", postURL)
	}
	pageURL := commentAnchorURL(&model.Post{ID: 7, PostType: model.PostTypePage, Slug: "guestbook"}, 11)
	if pageURL != "/guestbook#comment-11" {
		t.Fatalf("page comment anchor=%q", pageURL)
	}
	subject, body := commentReplyMail(gettext.NewTranslations(), "站点", "文章", "Alice", "Bob", "回复内容", "https://example.com/post#comment-1")
	if !strings.Contains(subject, "站点") || !strings.Contains(body, "Alice") || !strings.Contains(body, "Bob") || !strings.Contains(body, "https://example.com/post#comment-1") || !strings.Contains(body, "站点域名：example.com") {
		t.Fatalf("commentReplyMail subject=%q body=%q", subject, body)
	}
}

func nowForTest() time.Time { return time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC) }

func TestSaveSMTPSettingsDoesNotRequireSiteFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	h := &Admin{st: st}

	form := url.Values{}
	form.Set("smtp_host", "smtp.example.com")
	form.Set("smtp_port", "587")
	form.Set("smtp_user", "user@example.com")
	form.Set("smtp_password", "secret")
	form.Set("smtp_from", "noreply@example.com")
	form.Set("site_url", "https://example.com")
	w := httptest.NewRecorder()
	r := gin.New()
	r.POST("/admin/settings/smtp", h.SaveSMTPSettings)
	req := httptest.NewRequest(http.MethodPost, "/admin/settings/smtp", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("SaveSMTPSettings status=%d, want %d", w.Code, http.StatusSeeOther)
	}
	settings, err := st.GetSettings(context.Background(), consts.SettingsSMTPHost, consts.SettingsSMTPPort, consts.SettingsSMTPFrom, consts.SettingsSiteURL)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if settings[consts.SettingsSMTPHost] != "smtp.example.com" || settings[consts.SettingsSMTPPort] != "587" || settings[consts.SettingsSMTPFrom] != "noreply@example.com" || settings[consts.SettingsSiteURL] != "https://example.com" {
		t.Fatalf("smtp settings=%v", settings)
	}
}
