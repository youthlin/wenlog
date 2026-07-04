package handler

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
)

const maxThemeFileSize = 100 << 10 // 100KB

const (
	editableResourceTheme    = "theme"
	editableResourcePlugin   = "plugin"
	editableResourceTemplate = "template"
	editableResourceAsset    = "asset"
	editableResourceI18n     = "i18n"
)

type editableFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ThemeFilesPage 渲染主题/插件文件编辑器页面。
func (h *Admin) ThemeFilesPage(c *gin.Context) {
	tr := i18n.Get(c)
	kind := editableKind(c.Query("kind"))
	name := strings.TrimSpace(c.Query("theme"))
	if kind == editableResourceTheme && name == "" && h.themeManager != nil {
		if current := h.themeManager.Current(c); current != nil {
			name = current.Name
		}
	}
	pluginID := strings.TrimSpace(c.Query("plugin"))
	if kind == editableResourcePlugin && pluginID == "" && h.pluginManager != nil {
		plugins := h.pluginManager.List()
		if len(plugins) > 0 {
			pluginID = plugins[0].ID
		}
	}
	data := h.base(c, tr.T("编辑文件"))
	data["CurrentAdminNav"] = "theme-files"
	data["EditKind"] = kind
	data["EditTheme"] = name
	data["EditPlugin"] = pluginID
	data["EditResourceName"] = editableResourceName(kind, name, pluginID)
	data["CanReloadResource"] = editableResourceCanReload(kind)
	if h.themeManager != nil {
		data["Themes"] = h.themeManager.List()
		data["CurrentTheme"] = h.themeManager.Current(c)
		if kind == editableResourceTheme {
			files, err := h.themeManager.ListThemeFiles(name)
			if err == nil {
				data["ThemeFiles"] = files
			}
		}
	}
	if h.pluginManager != nil {
		data["Plugins"] = h.pluginManager.List()
		if kind == editableResourcePlugin {
			files, err := h.listPluginFiles(pluginID)
			if err == nil {
				data["ThemeFiles"] = files
			}
		}
	}
	if editableResourceIsLocal(kind) {
		files, err := h.listLocalEditableFiles(kind)
		if err == nil {
			data["ThemeFiles"] = files
		}
		if err != nil {
			data["EditorHint"] = tr.T("本地目录 %s 不存在，请先在开发设置中释放对应的内嵌资源。", editableResourceRoot(kind))
		}
	}
	// 恢复信息
	if h.themeManager != nil {
		data["RecoveryInfo"] = h.themeManager.GetRecoveryInfo()
	}
	c.HTML(http.StatusOK, "admin_edit_files.gohtml", data)
}

// ThemeFileRead 读取主题文件内容。
func (h *Admin) ThemeFileRead(c *gin.Context) {
	kind := editableKind(c.Query("kind"))
	name := strings.TrimSpace(c.Query("theme"))
	pluginID := strings.TrimSpace(c.Query("plugin"))
	path := strings.TrimSpace(c.Query("path"))
	if path == "" || !editableResourceHasTarget(kind, name, pluginID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource and path are required"})
		return
	}
	if !isEditableFilePathForKind(kind, path) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported file type"})
		return
	}
	fullPath, err := h.editableFilePath(kind, name, pluginID, path)
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

func editableKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case editableResourcePlugin:
		return editableResourcePlugin
	case editableResourceTemplate:
		return editableResourceTemplate
	case editableResourceAsset:
		return editableResourceAsset
	case editableResourceI18n:
		return editableResourceI18n
	default:
		return editableResourceTheme
	}
}

func (h *Admin) editableFilePath(kind, themeName, pluginID, relPath string) (string, error) {
	switch kind {
	case editableResourcePlugin:
		return h.pluginFilePath(pluginID, relPath)
	case editableResourceTemplate, editableResourceAsset, editableResourceI18n:
		return h.localEditableFilePath(kind, relPath)
	}
	tm := h.themeManager
	if tm == nil {
		return "", fmt.Errorf("theme manager not initialized")
	}
	return tm.ThemeFilePath(themeName, relPath)
}

func editableResourceIsLocal(kind string) bool {
	return kind == editableResourceTemplate || kind == editableResourceAsset || kind == editableResourceI18n
}

func editableResourceCanReload(kind string) bool {
	return kind != editableResourceAsset
}

func editableResourceName(kind, themeName, pluginID string) string {
	switch kind {
	case editableResourcePlugin:
		return pluginID
	case editableResourceTemplate:
		return editableResourceRoot(kind)
	case editableResourceAsset:
		return editableResourceRoot(kind)
	case editableResourceI18n:
		return editableResourceRoot(kind)
	default:
		return themeName
	}
}

func editableResourceRoot(kind string) string {
	switch kind {
	case editableResourceTemplate:
		return filepath.Join("web", "templates")
	case editableResourceAsset:
		return filepath.Join("web", "assets")
	case editableResourceI18n:
		return filepath.Join("web", "i18n")
	default:
		return ""
	}
}

func (h *Admin) localEditableFilePath(kind, relPath string) (string, error) {
	root := editableResourceRoot(kind)
	if root == "" {
		return "", fmt.Errorf("unsupported resource kind %q", kind)
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("local resource dir %q not found", root)
		}
		return "", err
	}
	return safeFilePathUnderRoot(root, relPath)
}

func (h *Admin) pluginFilePath(id, relPath string) (string, error) {
	if h.pluginManager == nil {
		return "", fmt.Errorf("plugin manager not initialized")
	}
	p := h.pluginManager.Get(id)
	if p == nil {
		return "", fmt.Errorf("plugin %q not found", id)
	}
	return safeFilePathUnderRoot(p.Dir, relPath)
}

func (h *Admin) listPluginFiles(id string) ([]editableFile, error) {
	if h.pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not initialized")
	}
	p := h.pluginManager.Get(id)
	if p == nil {
		return nil, fmt.Errorf("plugin %q not found", id)
	}
	var files []editableFile
	err := filepath.WalkDir(p.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(p.Dir, path)
		if err != nil {
			return err
		}
		if !isEditableThemeFilePath(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, editableFile{Path: rel, Size: info.Size()})
		return nil
	})
	return files, err
}

func (h *Admin) listLocalEditableFiles(kind string) ([]editableFile, error) {
	root := editableResourceRoot(kind)
	if root == "" {
		return nil, fmt.Errorf("unsupported resource kind %q", kind)
	}
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	var files []editableFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !isEditableLocalFilePath(kind, rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, editableFile{Path: rel, Size: info.Size()})
		return nil
	})
	return files, err
}

func safeFilePathUnderRoot(root, relPath string) (string, error) {
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("absolute path is not allowed: %s", relPath)
	}
	clean := filepath.Clean(relPath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal detected: %s", relPath)
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullPath, err := filepath.Abs(filepath.Join(base, clean))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, fullPath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path traversal detected: %s", relPath)
	}
	return fullPath, nil
}

// ThemeFileSave 保存主题文件内容。
func (h *Admin) ThemeFileSave(c *gin.Context) {
	tr := i18n.Get(c)
	var req struct {
		Kind    string `json:"kind"`
		Theme   string `json:"theme"`
		Plugin  string `json:"plugin"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kind := editableKind(req.Kind)
	if req.Path == "" || !editableResourceHasTarget(kind, req.Theme, req.Plugin) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource and path are required"})
		return
	}
	if !isEditableFilePathForKind(kind, req.Path) {
		c.JSON(http.StatusBadRequest, gin.H{"error": tr.T("不支持的文件类型")})
		return
	}
	if len(req.Content) > maxThemeFileSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": tr.T("文件内容不能超过 100KB")})
		return
	}
	fullPath, err := h.editableFilePath(kind, req.Theme, req.Plugin, req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 写入文件
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create dir: " + err.Error()})
		return
	}
	if err := os.WriteFile(fullPath, []byte(req.Content), 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write file: " + err.Error()})
		return
	}
	reloaded, reloadErr := h.reloadAfterEditableSave(c, kind, req.Path)
	if reloadErr != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":       true,
			"reloaded": false,
			"error":    tr.T("文件已保存，但重载失败: %s", reloadErr.Error()),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"reloaded": reloaded,
	})
}

func editableResourceHasTarget(kind, themeName, pluginID string) bool {
	switch kind {
	case editableResourcePlugin:
		return strings.TrimSpace(pluginID) != ""
	case editableResourceTheme:
		return strings.TrimSpace(themeName) != ""
	case editableResourceTemplate, editableResourceAsset, editableResourceI18n:
		return true
	default:
		return false
	}
}

func (h *Admin) reloadAfterEditableSave(c *gin.Context, kind, path string) (bool, error) {
	base := filepath.Base(path)
	switch kind {
	case editableResourcePlugin:
		if base == "functions.go" || base == "functions.goyaegi" {
			return true, h.reloadPluginRuntime(c)
		}
	case editableResourceTheme:
		if base == "functions.go" || base == "functions.goyaegi" || base == "theme.yaml" {
			return h.reloadLocalResource(c, kind)
		}
	case editableResourceTemplate:
		return h.reloadLocalResource(c, kind)
	case editableResourceI18n:
		return h.reloadLocalResource(c, kind)
	}
	return false, nil
}

// ThemeFileCreate 新建主题文件。
func (h *Admin) ThemeFileCreate(c *gin.Context) {
	tr := i18n.Get(c)
	var req struct {
		Kind    string `json:"kind"`
		Theme   string `json:"theme"`
		Plugin  string `json:"plugin"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Path = strings.TrimSpace(req.Path)
	kind := editableKind(req.Kind)
	if req.Path == "" || !editableResourceHasTarget(kind, req.Theme, req.Plugin) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource and path are required"})
		return
	}
	if !isEditableFilePathForKind(kind, req.Path) {
		c.JSON(http.StatusBadRequest, gin.H{"error": tr.T("不支持的文件类型")})
		return
	}
	// 安全检查：不允许路径穿越
	if strings.Contains(req.Path, "..") || filepath.IsAbs(req.Path) {
		c.JSON(http.StatusBadRequest, gin.H{"error": tr.T("无效的文件路径")})
		return
	}
	// 只允许可编辑的文件类型
	if !isEditableFilePathForKind(kind, req.Path) {
		c.JSON(http.StatusBadRequest, gin.H{"error": tr.T("不支持的文件类型")})
		return
	}
	fullPath, err := h.editableFilePath(kind, req.Theme, req.Plugin, req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 检查文件是否已存在
	if _, err := os.Stat(fullPath); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": tr.T("文件已存在")})
		return
	}
	// 确保父目录存在
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create dir: " + err.Error()})
		return
	}
	// 写入文件
	if err := os.WriteFile(fullPath, []byte(req.Content), 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write file: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "path": req.Path})
}

// isEditableThemeFilePath 检查新建文件路径是否属于可编辑类型。
func isEditableThemeFilePath(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".gohtml", ".html", ".css", ".js", ".yaml", ".yml", ".po", ".mo":
		return true
	case ".go", ".goyaegi":
		base := filepath.Base(path)
		return base == "functions.go" || base == "functions.goyaegi"
	default:
		return false
	}
}

func isEditableFilePathForKind(kind, path string) bool {
	if editableResourceIsLocal(kind) {
		return isEditableLocalFilePath(kind, path)
	}
	return isEditableThemeFilePath(path)
}

func isEditableLocalFilePath(kind, path string) bool {
	ext := filepath.Ext(path)
	switch kind {
	case editableResourceTemplate:
		return ext == ".gohtml"
	case editableResourceAsset:
		switch ext {
		case ".css", ".js", ".json", ".map", ".svg", ".txt", ".xml", ".webmanifest":
			return true
		default:
			return false
		}
	case editableResourceI18n:
		return ext == ".po" || ext == ".pot"
	default:
		return false
	}
}

// ThemeFileDelete 删除主题文件。
func (h *Admin) ThemeFileDelete(c *gin.Context) {
	tr := i18n.Get(c)
	var req struct {
		Kind   string `json:"kind"`
		Theme  string `json:"theme"`
		Plugin string `json:"plugin"`
		Path   string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kind := editableKind(req.Kind)
	if req.Path == "" || !editableResourceHasTarget(kind, req.Theme, req.Plugin) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource and path are required"})
		return
	}
	if !isEditableFilePathForKind(kind, req.Path) {
		c.JSON(http.StatusBadRequest, gin.H{"error": tr.T("不支持的文件类型")})
		return
	}
	// 不允许删除核心文件
	base := filepath.Base(req.Path)
	if base == "theme.yaml" || base == "plugin.yaml" || base == "functions.go" || base == "functions.goyaegi" {
		c.JSON(http.StatusBadRequest, gin.H{"error": tr.T("不能删除核心文件 %s", base)})
		return
	}
	fullPath, err := h.editableFilePath(kind, req.Theme, req.Plugin, req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := os.Remove(fullPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete file: " + err.Error()})
		return
	}
	reloaded, reloadErr := h.reloadAfterEditableSave(c, kind, req.Path)
	if reloadErr != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "reloaded": false, "error": tr.T("文件已删除，但重载失败: %s", reloadErr.Error())})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "reloaded": reloaded})
}

// ThemeFileReload 重载当前编辑器资源。
func (h *Admin) ThemeFileReload(c *gin.Context) {
	tr := i18n.Get(c)
	var req struct {
		Kind   string `json:"kind"`
		Theme  string `json:"theme"`
		Plugin string `json:"plugin"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kind := editableKind(req.Kind)
	if !editableResourceHasTarget(kind, req.Theme, req.Plugin) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource is required"})
		return
	}
	reloaded, err := h.reloadLocalResource(c, kind)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": tr.T("重载失败: %s", err.Error())})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "reloaded": reloaded})
}

func (h *Admin) reloadLocalResource(c *gin.Context, kind string) (bool, error) {
	switch kind {
	case editableResourcePlugin:
		return true, h.reloadPluginRuntime(c)
	case editableResourceTheme:
		if h.themeManager == nil {
			return false, fmt.Errorf("theme manager not initialized")
		}
		return true, h.themeManager.ReloadCurrentTheme(c)
	case editableResourceTemplate:
		if h.renderer == nil || !h.renderer.Hot() {
			return false, fmt.Errorf("template hot reload is not enabled")
		}
		return true, h.renderer.Reload()
	case editableResourceAsset:
		return false, nil
	case editableResourceI18n:
		if !i18n.Hot() {
			return false, fmt.Errorf("i18n hot reload is not enabled")
		}
		if err := i18n.Reload(); err != nil {
			return false, err
		}
		if h.themeManager != nil {
			if err := h.themeManager.LoadTranslations(); err != nil {
				return false, err
			}
		}
		if h.pluginManager != nil {
			if err := h.pluginManager.LoadTranslations(); err != nil {
				return false, err
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported resource kind %q", kind)
	}
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
