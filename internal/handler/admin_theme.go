package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	gettext "github.com/youthlin/t"
	"github.com/youthlin/wenlog/internal/extension"
	"github.com/youthlin/wenlog/internal/i18n"
	"github.com/youthlin/wenlog/internal/middleware"
	"github.com/youthlin/wenlog/internal/theme"
)

const (
	maxThemeUploadSize     = 5 << 20  // 5MB
	maxThemeExtractedSize  = 20 << 20 // 20MB
	maxThemeExtractedFile  = 2 << 20  // 2MB
	maxThemeExtractedFiles = 500
	maxThemeExtractedName  = 255
)

// ThemesPage 渲染主题管理页面。
func (h *Admin) ThemesPage(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("外观"))
	data["CurrentAdminNav"] = "themes"
	if h.themeManager != nil {
		data["Themes"] = translateThemeList(tr, h.themeManager.List())
		data["CurrentTheme"] = toThemeView(tr, h.themeManager.Current(c))
		// 预览主题信息
		if previewName := middleware.GetPreviewTheme(c); previewName != "" {
			data["PreviewTheme"] = toThemeView(tr, h.themeManager.Get(previewName))
		}
	}
	if msg := c.Query("message"); msg != "" {
		data["Notice"] = msg
	}
	c.HTML(http.StatusOK, "admin_themes.gohtml", data)
}

// themeView 是主题列表页的展示模型，包含翻译后的字段。
type themeView struct {
	*theme.Theme
	Description string   // 已翻译
	Tags        []string // 已翻译
}

func translateThemeList(tr *gettext.Translations, themes []*theme.Theme) []*themeView {
	result := make([]*themeView, 0, len(themes))
	for _, t := range themes {
		if t == nil {
			continue
		}
		result = append(result, toThemeView(tr, t))
	}
	return result
}

func toThemeView(tr *gettext.Translations, t *theme.Theme) *themeView {
	if t == nil {
		return nil
	}
	th := tr.D(t.ThemeDomain())
	desc := t.Description
	if desc != "" {
		if translated := th.T(desc); translated != "" {
			desc = translated
		}
	}
	tags := make([]string, len(t.Tags))
	for i, tag := range t.Tags {
		tags[i] = tag
		if tag != "" {
			if translated := th.T(tag); translated != "" {
				tags[i] = translated
			}
		}
	}
	return &themeView{
		Theme:       t,
		Description: desc,
		Tags:        tags,
	}
}

// ThemeUpload 处理主题 zip 上传。
func (h *Admin) ThemeUpload(c *gin.Context) {
	tr := i18n.Get(c)
	fh, err := c.FormFile("theme_zip")
	if err != nil {
		h.redirectThemeSettings(c, tr.T("未选择文件"))
		return
	}
	if fh.Size > maxThemeUploadSize {
		h.redirectThemeSettings(c, tr.T("主题包不能超过 5MB"))
		return
	}
	if !strings.HasSuffix(strings.ToLower(fh.Filename), ".zip") {
		h.redirectThemeSettings(c, tr.T("仅支持 .zip 格式的主题包"))
		return
	}

	file, err := fh.Open()
	if err != nil {
		h.redirectThemeSettings(c, tr.T("打开上传文件失败"))
		return
	}
	defer file.Close()

	// 解压到临时目录
	tmpDir, err := os.MkdirTemp("", "theme-upload-*")
	if err != nil {
		h.redirectThemeSettings(c, tr.T("创建临时目录失败"))
		return
	}
	defer os.RemoveAll(tmpDir)

	if err := extension.ExtractZip(file, fh.Size, tmpDir, extension.ExtractOptions{
		Kind:       "theme",
		MaxSize:    maxThemeExtractedSize,
		MaxFile:    maxThemeExtractedFile,
		MaxFiles:   maxThemeExtractedFiles,
		MaxNameLen: maxThemeExtractedName,
		AllowFile:  isAllowedThemeFile,
	}); err != nil {
		h.redirectThemeSettings(c, tr.T("解压主题包失败: %s", err.Error()))
		return
	}

	// 查找 theme.yaml 所在目录（可能在子目录中）
	themeRoot, err := findThemeRoot(tmpDir)
	if err != nil {
		h.redirectThemeSettings(c, tr.T("主题包中未找到 theme.yaml"))
		return
	}

	// 安装主题
	tm := h.themeManager
	if tm == nil {
		h.redirectThemeSettings(c, tr.T("主题管理器未初始化"))
		return
	}
	installed, err := tm.Install(themeRoot)
	if err != nil {
		h.redirectThemeSettings(c, tr.T("安装主题失败: %s", err.Error()))
		return
	}

	h.redirectThemeSettings(c, tr.T("主题「%s」安装成功", installed.Name))
}

// ThemeActivate 激活指定主题。
func (h *Admin) ThemeActivate(c *gin.Context) {
	tr := i18n.Get(c)
	name := strings.TrimSpace(c.PostForm("theme_name"))
	if name == "" {
		h.redirectThemeSettings(c, tr.T("未指定主题名称"))
		return
	}
	tm := h.themeManager
	if tm == nil {
		h.redirectThemeSettings(c, tr.T("主题管理器未初始化"))
		return
	}
	if err := tm.Activate(c, name); err != nil {
		h.redirectThemeSettings(c, tr.T("激活主题失败: %s", err.Error()))
		return
	}
	// 加载模板 + functions.go（含恢复机制）
	if err := tm.LoadTheme(c, name); err != nil {
		h.redirectThemeSettings(c, tr.T("主题已激活，但加载失败，已回退默认主题: %s", err.Error()))
		return
	}
	middleware.ClearPreviewTheme(c)
	tm.ClearPreviewTheme()
	h.redirectThemeSettings(c, tr.T("已激活主题「%s」", name))
}

// ThemeDelete 删除指定主题。
func (h *Admin) ThemeDelete(c *gin.Context) {
	tr := i18n.Get(c)
	name := strings.TrimSpace(c.PostForm("theme_name"))
	if name == "" {
		h.redirectThemeSettings(c, tr.T("未指定主题名称"))
		return
	}
	tm := h.themeManager
	if tm == nil {
		h.redirectThemeSettings(c, tr.T("主题管理器未初始化"))
		return
	}
	wasCurrent := false
	if current := tm.Current(c); current != nil && current.Name == name {
		wasCurrent = true
	}
	if err := tm.Delete(name); err != nil {
		h.redirectThemeSettings(c, tr.T("删除主题失败: %s", err.Error()))
		return
	}
	// 如果删除的是当前主题，回退到默认
	if wasCurrent {
		_ = tm.Activate(c, "default")
		_ = tm.LoadTheme(c, "default")
	}
	h.redirectThemeSettings(c, tr.T("主题「%s」已删除", name))
}

// ThemeDownload 下载指定主题为 zip 包。
func (h *Admin) ThemeDownload(c *gin.Context) {
	tr := i18n.Get(c)
	name := strings.TrimSpace(c.Query("theme"))
	if name == "" {
		h.redirectThemeSettings(c, tr.T("未指定主题名称"))
		return
	}
	tm := h.themeManager
	if tm == nil {
		h.redirectThemeSettings(c, tr.T("主题管理器未初始化"))
		return
	}
	t := tm.Get(name)
	if t == nil {
		h.redirectThemeSettings(c, tr.T("主题「%s」不存在", name))
		return
	}
	tmp, err := os.CreateTemp("", "theme-download-*.zip")
	if err != nil {
		h.redirectThemeSettings(c, tr.T("创建主题下载文件失败"))
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := extension.WriteZip(tmp, t.Name, t.Dir); err != nil {
		_ = tmp.Close()
		h.redirectThemeSettings(c, tr.T("打包主题失败: %s", err.Error()))
		return
	}
	if err := tmp.Close(); err != nil {
		h.redirectThemeSettings(c, tr.T("写入主题下载文件失败: %s", err.Error()))
		return
	}
	c.FileAttachment(tmpName, t.Name+".zip")

}

func (h *Admin) redirectThemeSettings(c *gin.Context, msg string) {
	u := "/admin/themes?message=" + url.QueryEscape(msg)
	c.Redirect(http.StatusSeeOther, u)
}

// ThemePreview 设置管理员主题预览（写入 session，不持久化到 DB）。
func (h *Admin) ThemePreview(c *gin.Context) {
	tr := i18n.Get(c)
	name := strings.TrimSpace(c.PostForm("theme_name"))
	if name == "" {
		h.redirectThemeSettings(c, tr.T("未指定主题名称"))
		return
	}
	tm := h.themeManager
	if tm == nil {
		h.redirectThemeSettings(c, tr.T("主题管理器未初始化"))
		return
	}
	if tm.Get(name) == nil {
		h.redirectThemeSettings(c, tr.T("主题「%s」不存在", name))
		return
	}
	// 加载预览主题模板到 Renderer 独立缓存（不影响主模板，访客不受影响）
	if err := tm.LoadPreviewTheme(name); err != nil {
		h.redirectThemeSettings(c, tr.T("加载预览主题失败: %s", err.Error()))
		return
	}
	middleware.SetPreviewTheme(c, name)
	h.redirectThemeSettings(c, tr.T("正在预览主题「%s」，前台已生效。", name))
}

// ThemePreviewClear 清除管理员主题预览。
func (h *Admin) ThemePreviewClear(c *gin.Context) {
	tr := i18n.Get(c)
	middleware.ClearPreviewTheme(c)
	if h.themeManager != nil {
		h.themeManager.ClearPreviewTheme()
	}
	h.redirectThemeSettings(c, tr.T("已退出主题预览"))
}

// ThemeScreenshot 提供主题截图文件。
func (h *Admin) ThemeScreenshot(c *gin.Context) {
	name := c.Param("name")
	file := c.Param("file")
	if name == "" || file == "" {
		c.Status(http.StatusNotFound)
		return
	}
	tm := h.themeManager
	if tm == nil {
		c.Status(http.StatusNotFound)
		return
	}
	t := tm.Get(name)
	if t == nil {
		c.Status(http.StatusNotFound)
		return
	}
	// 安全检查：只允许文件名
	base := filepath.Base(file)
	if base != file || base == "." || base == ".." {
		c.Status(http.StatusNotFound)
		return
	}
	c.File(filepath.Join(t.Dir, base))
}

// isAllowedThemeFile 检查文件扩展名是否在主题文件白名单中。
func isAllowedThemeFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".go", ".goyaegi", ".gohtml", ".html", ".css", ".js", ".json", ".yaml", ".yml", ".po", ".mo",
		".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico",
		".woff", ".woff2", ".ttf", ".eot":
		return true
	default:
		return false
	}
}

// findThemeRoot 在解压目录中查找 theme.yaml 所在目录。
// 支持两种情况：theme.yaml 在根目录，或在子目录中（如 my-theme/theme.yaml）。
func findThemeRoot(dir string) (string, error) {
	// 先检查根目录
	if _, err := os.Stat(filepath.Join(dir, "theme.yaml")); err == nil {
		return dir, nil
	}
	// 检查一级子目录
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sub := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(filepath.Join(sub, "theme.yaml")); err == nil {
			return sub, nil
		}
	}
	return "", fmt.Errorf("theme.yaml not found in %s", dir)
}
