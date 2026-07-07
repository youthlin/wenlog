package main

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	gettext "github.com/youthlin/t"
	"golang.org/x/crypto/bcrypt"

	"github.com/youthlin/wenlog/internal/handler"
	"github.com/youthlin/wenlog/internal/i18n"
	"github.com/youthlin/wenlog/internal/model"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/web"
	"github.com/youthlin/wenlog/web/plugins"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return st
}

func TestEnsureInitialAdmin(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := ensureInitialAdmin(st); err != nil {
		t.Fatalf("ensure initial admin: %v", err)
	}
	n, err := st.CountUsers(ctx)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Fatalf("user count = %d, want 1", n)
	}
	u, err := st.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if u.PasswordHash == "" {
		t.Fatal("admin password hash is empty")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("")) == nil {
		t.Fatal("empty password unexpectedly matches")
	}
	if err := ensureInitialAdmin(st); err != nil {
		t.Fatalf("ensure initial admin second run: %v", err)
	}
	n, err = st.CountUsers(ctx)
	if err != nil {
		t.Fatalf("count users second run: %v", err)
	}
	if n != 1 {
		t.Fatalf("user count after second run = %d, want 1", n)
	}
}

func TestLocalFirstFileSystemSwitchesToLocalDir(t *testing.T) {
	localDir := filepath.Join(t.TempDir(), "assets")
	fallback := http.FS(fstest.MapFS{
		"style.css": &fstest.MapFile{Data: []byte("from-embed")},
	})
	fsys := handler.NewLocalFirstFileSystem(localDir, fallback)

	if got := readFileFromHTTPFS(t, fsys, "style.css"); got != "from-embed" {
		t.Fatalf("expected fallback content, got %q", got)
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatalf("mkdir local assets dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "style.css"), []byte("from-local"), 0o644); err != nil {
		t.Fatalf("write local asset: %v", err)
	}
	fsys.SetHot(true)
	if got := readFileFromHTTPFS(t, fsys, "style.css"); got != "from-local" {
		t.Fatalf("expected local content, got %q", got)
	}
}

func readFileFromHTTPFS(t *testing.T, fsys http.FileSystem, name string) string {
	t.Helper()
	f, err := fsys.Open(name)
	if err != nil {
		t.Fatalf("open %q: %v", name, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read %q: %v", name, err)
	}
	return string(data)
}

func TestEnsureInitialContent(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if err := i18n.Init(); err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	gettext.SetLocale("en_US")
	if err := ensureInitialAdmin(st); err != nil {
		t.Fatalf("ensure initial admin: %v", err)
	}
	if err := ensureInitialContent(st); err != nil {
		t.Fatalf("ensure initial content: %v", err)
	}
	if err := ensureInitialContent(st); err != nil {
		t.Fatalf("ensure initial content second run: %v", err)
	}
	var posts []model.Post
	if err := st.DB(ctx).Order("id ASC").Find(&posts).Error; err != nil {
		t.Fatalf("list posts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("post count = %d, want 2", len(posts))
	}
	if posts[0].Title != "Welcome to WenLog" || posts[0].PostType != model.PostTypePost {
		t.Fatalf("first post = %+v", posts[0])
	}
	var categories []model.Category
	if err := st.DB(ctx).Order("id ASC").Find(&categories).Error; err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(categories) != 1 || categories[0].Name != "Uncategorized" || categories[0].Slug != "uncategorized" {
		t.Fatalf("categories = %+v", categories)
	}
	post, err := st.GetPostByID(context.Background(), posts[0].ID)
	if err != nil {
		t.Fatalf("get welcome post: %v", err)
	}
	if len(post.Categories) != 1 || post.Categories[0].Slug != "uncategorized" {
		t.Fatalf("welcome post categories = %+v", post.Categories)
	}
	if posts[1].Title != "About" || posts[1].Slug != "about" || posts[1].MenuOrder != 1 || posts[1].PostType != model.PostTypePage {
		t.Fatalf("about page = %+v", posts[1])
	}
	var comments []model.Comment
	if err := st.DB(ctx).Order("id ASC").Find(&comments).Error; err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comment count = %d, want 1", len(comments))
	}
	if comments[0].Status != model.CommentApproved || comments[0].Author != "youthlin" {
		t.Fatalf("comment = %+v", comments[0])
	}
}

func TestEnsureThemesOnDiskRefreshesBundledThemeWhenVersionChanged(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	staleDir := filepath.Join("themes", "spread", "templates")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatalf("mkdir stale theme dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join("themes", "spread", "theme.yaml"), []byte("name: spread\nversion: 0.0.0\n"), 0o644); err != nil {
		t.Fatalf("write stale theme yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "header.gohtml"), []byte("stale-header"), 0o644); err != nil {
		t.Fatalf("write stale header: %v", err)
	}

	ensureThemesOnDisk()

	wantYAML, err := fs.ReadFile(web.Themes, "themes/spread/theme.yaml")
	if err != nil {
		t.Fatalf("read bundled theme yaml: %v", err)
	}
	gotYAML, err := os.ReadFile(filepath.Join("themes", "spread", "theme.yaml"))
	if err != nil {
		t.Fatalf("read refreshed theme yaml: %v", err)
	}
	if string(gotYAML) != string(wantYAML) {
		t.Fatalf("theme yaml not refreshed: got %q want %q", gotYAML, wantYAML)
	}

	wantHeader, err := fs.ReadFile(web.Themes, "themes/spread/templates/header.gohtml")
	if err != nil {
		t.Fatalf("read bundled header: %v", err)
	}
	gotHeader, err := os.ReadFile(filepath.Join("themes", "spread", "templates", "header.gohtml"))
	if err != nil {
		t.Fatalf("read refreshed header: %v", err)
	}
	if string(gotHeader) != string(wantHeader) {
		t.Fatalf("theme template not refreshed")
	}
	if !strings.Contains(string(gotHeader), `slot "head.end"`) {
		t.Fatalf("refreshed header missing plugin hook: %s", gotHeader)
	}
}

func TestEnsurePluginsOnDiskReleasesBundledPlugins(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	ensurePluginsOnDisk()

	wantYAML, err := fs.ReadFile(plugins.Plugins, "post-comment-enhance/plugin.yaml")
	if err != nil {
		t.Fatalf("read bundled plugin yaml: %v", err)
	}
	gotYAML, err := os.ReadFile(filepath.Join("plugins", "post-comment-enhance", "plugin.yaml"))
	if err != nil {
		t.Fatalf("read released plugin yaml: %v", err)
	}
	if string(gotYAML) != string(wantYAML) {
		t.Fatalf("plugin yaml not released: got %q want %q", gotYAML, wantYAML)
	}

	wantWidget, err := fs.ReadFile(plugins.Plugins, "common-widgets/widgets/saying.gohtml")
	if err != nil {
		t.Fatalf("read bundled plugin widget: %v", err)
	}
	gotWidget, err := os.ReadFile(filepath.Join("plugins", "common-widgets", "widgets", "saying.gohtml"))
	if err != nil {
		t.Fatalf("read released plugin widget: %v", err)
	}
	if string(gotWidget) != string(wantWidget) {
		t.Fatalf("plugin widget not released")
	}
}

func TestEnsurePluginsOnDiskRefreshesBundledPluginWhenVersionChanged(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	pluginDir := filepath.Join("plugins", "common-widgets")
	widgetDir := filepath.Join(pluginDir, "widgets")
	if err := os.MkdirAll(widgetDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin widget dir: %v", err)
	}
	staleYAML := []byte("id: common-widgets\nname: 本地旧版本\nversion: 0.0.0\n")
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), staleYAML, 0o644); err != nil {
		t.Fatalf("write stale plugin yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(widgetDir, "saying.gohtml"), []byte("stale-widget"), 0o644); err != nil {
		t.Fatalf("write stale plugin widget: %v", err)
	}

	ensurePluginsOnDisk()

	wantYAML, err := fs.ReadFile(plugins.Plugins, "common-widgets/plugin.yaml")
	if err != nil {
		t.Fatalf("read bundled plugin yaml: %v", err)
	}
	gotYAML, err := os.ReadFile(filepath.Join(pluginDir, "plugin.yaml"))
	if err != nil {
		t.Fatalf("read refreshed plugin yaml: %v", err)
	}
	if string(gotYAML) != string(wantYAML) {
		t.Fatalf("plugin yaml not refreshed: got %q want %q", gotYAML, wantYAML)
	}

	wantWidget, err := fs.ReadFile(plugins.Plugins, "common-widgets/widgets/saying.gohtml")
	if err != nil {
		t.Fatalf("read bundled plugin widget: %v", err)
	}
	gotWidget, err := os.ReadFile(filepath.Join(widgetDir, "saying.gohtml"))
	if err != nil {
		t.Fatalf("read refreshed plugin widget: %v", err)
	}
	if string(gotWidget) != string(wantWidget) {
		t.Fatalf("plugin widget not refreshed")
	}
}

func TestEnsurePluginsOnDiskKeepsExistingPluginWhenNotOlder(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	pluginDir := filepath.Join("plugins", "common-widgets")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	customYAML := []byte("id: common-widgets\nname: 本地修改\nversion: 99999999999999\n")
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), customYAML, 0o644); err != nil {
		t.Fatalf("write custom plugin yaml: %v", err)
	}

	ensurePluginsOnDisk()

	gotYAML, err := os.ReadFile(filepath.Join(pluginDir, "plugin.yaml"))
	if err != nil {
		t.Fatalf("read custom plugin yaml: %v", err)
	}
	if string(gotYAML) != string(customYAML) {
		t.Fatalf("newer existing plugin should be kept: got %q want %q", gotYAML, customYAML)
	}
}
