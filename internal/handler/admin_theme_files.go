package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
)

const maxThemeFileSize = 100 << 10 // 100KB

// ThemeFilesPage 渲染主题文件编辑器页面。
func (h *Admin) ThemeFilesPage(c *gin.Context) {
	tr := i18n.Get(c)
	name := strings.TrimSpace(c.Query("theme"))
	if name == "" {
		if current := h.themeManager.Current(c); current != nil {
			name = current.Name
		}
	}
	data := h.base(c, tr.T("编辑主题文件"))
	data["CurrentAdminNav"] = "themes"
	data["EditTheme"] = name
	if h.themeManager != nil {
		data["Themes"] = h.themeManager.List()
		data["CurrentTheme"] = h.themeManager.Current(c)
		files, err := h.themeManager.ListThemeFiles(name)
		if err == nil {
			data["ThemeFiles"] = files
		}
	}
	// 恢复信息
	if h.themeManager != nil {
		data["RecoveryInfo"] = h.themeManager.GetRecoveryInfo()
	}
	c.HTML(http.StatusOK, "admin_theme_files.gohtml", data)
}

// ThemeFileRead 读取主题文件内容。
func (h *Admin) ThemeFileRead(c *gin.Context) {
	name := strings.TrimSpace(c.Query("theme"))
	path := strings.TrimSpace(c.Query("path"))
	if name == "" || path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "theme and path are required"})
		return
	}
	tm := h.themeManager
	if tm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "theme manager not initialized"})
		return
	}
	fullPath, err := tm.ThemeFilePath(name, path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"path":    path,
		"content": string(data),
	})
}

// ThemeFileSave 保存主题文件内容。
func (h *Admin) ThemeFileSave(c *gin.Context) {
	tr := i18n.Get(c)
	var req struct {
		Theme   string `json:"theme"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Theme == "" || req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "theme and path are required"})
		return
	}
	if len(req.Content) > maxThemeFileSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": tr.T("文件内容不能超过 100KB")})
		return
	}
	tm := h.themeManager
	if tm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "theme manager not initialized"})
		return
	}
	fullPath, err := tm.ThemeFilePath(req.Theme, req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 写入文件
	if err := os.WriteFile(fullPath, []byte(req.Content), 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write file: " + err.Error()})
		return
	}
	// 如果是 functions.go 或 functions.goyaegi，触发主题重载
	reloaded := false
	base := filepath.Base(req.Path)
	if base == "functions.go" || base == "functions.goyaegi" {
		if err := tm.ReloadCurrentTheme(c); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"ok":       true,
				"reloaded": false,
				"error":    tr.T("文件已保存，但主题重载失败: %s", err.Error()),
			})
			return
		}
		reloaded = true
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"reloaded": reloaded,
	})
}

// ThemeFileDelete 删除主题文件。
func (h *Admin) ThemeFileDelete(c *gin.Context) {
	tr := i18n.Get(c)
	var req struct {
		Theme string `json:"theme"`
		Path  string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Theme == "" || req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "theme and path are required"})
		return
	}
	// 不允许删除核心文件
	base := filepath.Base(req.Path)
	if base == "theme.yaml" || base == "functions.go" {
		c.JSON(http.StatusBadRequest, gin.H{"error": tr.T("不能删除核心文件 %s", base)})
		return
	}
	tm := h.themeManager
	if tm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "theme manager not initialized"})
		return
	}
	fullPath, err := tm.ThemeFilePath(req.Theme, req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := os.Remove(fullPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete file: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ThemeRecoveryClear 清除恢复信息（管理员确认后）。
func (h *Admin) ThemeRecoveryClear(c *gin.Context) {
	if h.themeManager != nil {
		h.themeManager.ClearRecoveryInfo()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ThemeReload 手动重载当前主题。
func (h *Admin) ThemeReload(c *gin.Context) {
	tr := i18n.Get(c)
	tm := h.themeManager
	if tm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "theme manager not initialized"})
		return
	}
	if err := tm.ReloadCurrentTheme(c); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":    false,
			"error": tr.T("主题重载失败: %s", err.Error()),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
