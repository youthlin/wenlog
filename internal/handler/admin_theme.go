package handler

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	gettext "github.com/youthlin/t"
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

	if err := extractThemeZip(file, fh.Size, tmpDir); err != nil {
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
	if err := writeThemeZip(tmp, t.Name, t.Dir); err != nil {
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

func writeThemeZip(w io.Writer, rootName, dir string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(rootName, rel))
		if d.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
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

// extractThemeZip 解压 zip 到目标目录，带路径穿越防护。
func extractThemeZip(r io.ReaderAt, size int64, dest string) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	var total int64
	var files int
	for _, f := range zr.File {
		// 路径穿越防护
		name := filepath.Clean(f.Name)
		if !safeThemeZipPath(name) {
			continue
		}
		if len(filepath.Base(name)) > maxThemeExtractedName {
			return fmt.Errorf("theme file name too long: %s", name)
		}
		target := filepath.Join(dest, name)
		// 确保目标路径在 dest 内
		if !pathWithinDir(dest, target) {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		files++
		if files > maxThemeExtractedFiles {
			return fmt.Errorf("theme package contains too many files")
		}
		headerSize := int64(f.UncompressedSize64)
		if headerSize > maxThemeExtractedFile {
			return fmt.Errorf("theme file %s is too large", name)
		}
		// 创建父目录
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		// 只允许安全文件类型
		if !isAllowedThemeFile(name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		written, err := copyThemeZipFile(out, rc)
		rc.Close()
		closeErr := out.Close()
		if err != nil {
			_ = os.Remove(target)
			return err
		}
		if closeErr != nil {
			_ = os.Remove(target)
			return closeErr
		}
		total += written
		if total > maxThemeExtractedSize {
			_ = os.Remove(target)
			return fmt.Errorf("theme package uncompressed size is too large")
		}
	}
	return nil
}

func copyThemeZipFile(out io.Writer, rc io.Reader) (int64, error) {
	lr := &io.LimitedReader{R: rc, N: maxThemeExtractedFile + 1}
	written, err := io.Copy(out, lr)
	if err != nil {
		return written, err
	}
	if written > maxThemeExtractedFile {
		return written, fmt.Errorf("theme file is too large")
	}
	return written, nil
}

func safeThemeZipPath(name string) bool {
	if name == "." || name == ".." || filepath.IsAbs(name) {
		return false
	}
	return !strings.HasPrefix(name, ".."+string(filepath.Separator))
}

func pathWithinDir(dir, target string) bool {
	base, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	full, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, full)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
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
