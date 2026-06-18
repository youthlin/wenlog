package handler

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
)

const maxThemeUploadSize = 5 << 20 // 5MB

// ThemesPage 渲染主题管理页面。
func (h *Admin) ThemesPage(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("外观"))
	data["CurrentAdminNav"] = "themes"
	if h.themeManager != nil {
		data["Themes"] = h.themeManager.List()
		data["CurrentTheme"] = h.themeManager.Current(c)
	}
	if msg := c.Query("message"); msg != "" {
		data["Notice"] = msg
	}
	c.HTML(http.StatusOK, "admin_themes.gohtml", data)
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
	// 加载主题模板到 renderer
	t := tm.Get(name)
	if t != nil && t.HasTemplates() && h.renderer != nil {
		if err := h.renderer.LoadTheme(t.TemplatesDir()); err != nil {
			h.redirectThemeSettings(c, tr.T("加载主题模板失败: %s", err.Error()))
			return
		}
	} else if h.renderer != nil {
		if err := h.renderer.ResetToDefault(); err != nil {
			h.redirectThemeSettings(c, tr.T("重置模板失败: %s", err.Error()))
			return
		}
	}
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
	}
	current := tm.Current(c)
	if current != nil && current.HasTemplates() && h.renderer != nil {
		if err := h.renderer.LoadTheme(current.TemplatesDir()); err != nil {
			_ = h.renderer.ResetToDefault()
		}
	} else if h.renderer != nil {
		_ = h.renderer.ResetToDefault()
	}
	h.redirectThemeSettings(c, tr.T("主题「%s」已删除", name))
}

// ThemeReset 重置为默认主题（不使用任何主题模板）。
func (h *Admin) ThemeReset(c *gin.Context) {
	tr := i18n.Get(c)
	if h.themeManager != nil {
		if err := h.themeManager.Activate(c, "default"); err != nil {
			h.redirectThemeSettings(c, tr.T("切换默认主题失败: %s", err.Error()))
			return
		}
	}
	if h.renderer != nil {
		if err := h.renderer.ResetToDefault(); err != nil {
			h.redirectThemeSettings(c, tr.T("重置模板失败: %s", err.Error()))
			return
		}
	}
	h.redirectThemeSettings(c, tr.T("已重置为默认模板"))
}

func (h *Admin) redirectThemeSettings(c *gin.Context, msg string) {
	u := "/admin/themes?message=" + url.QueryEscape(msg)
	c.Redirect(http.StatusSeeOther, u)
}

// extractThemeZip 解压 zip 到目标目录，带路径穿越防护。
func extractThemeZip(r io.ReaderAt, size int64, dest string) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		// 路径穿越防护
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, ".."+string(filepath.Separator)) || strings.HasPrefix(name, "..") {
			continue
		}
		target := filepath.Join(dest, name)
		// 确保目标路径在 dest 内
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(filepath.Separator)) {
			continue
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		// 创建父目录
		os.MkdirAll(filepath.Dir(target), 0o755)
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
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// isAllowedThemeFile 检查文件扩展名是否在主题文件白名单中。
func isAllowedThemeFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".gohtml", ".html", ".css", ".js", ".json", ".yaml", ".yml", ".po", ".mo",
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
