package handler

import (
	"archive/zip"
	"context"
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

	"github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/plugin"
	"github.com/youthlin/blog/internal/theme"
)

const (
	maxPluginUploadSize     = 5 << 20  // 5MB
	maxPluginExtractedSize  = 20 << 20 // 20MB
	maxPluginExtractedFile  = 2 << 20  // 2MB
	maxPluginExtractedFiles = 500
	maxPluginExtractedName  = 255
)

// pluginView 是后台插件列表页的展示模型。
type pluginView struct {
	*plugin.Plugin
	Name         string
	Description  string
	Enabled      bool
	HasAssets    bool
	LoadError    string
	ActionNames  []string
	FilterNames  []string
	SlotNames    []string
	WidgetNames  []string
	OptionNames []string
	Source       string
}

type pluginOptionData struct {
	plugin.OptionDecl
	Value string
}

// PluginsPage 渲染插件管理页面。
func (h *Admin) PluginsPage(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("插件"))
	data["CurrentAdminNav"] = "plugins"
	if h.pluginManager != nil {
		data["Plugins"] = translatePluginList(tr, h.pluginManager.List(), h.enabledPluginSet(c), h.pluginManager.LoadErrors())
		data["EnabledPluginIDs"] = strings.Join(h.pluginManager.EnabledIDs(c), ", ")
	}
	if msg := c.Query("message"); msg != "" {
		data["Notice"] = msg
	}
	if msg := c.Query("error"); msg != "" {
		data["Error"] = msg
	}
	c.HTML(http.StatusOK, "admin_plugins.gohtml", data)
}

// PluginAction 处理插件启用、停用和重载。
func (h *Admin) PluginAction(c *gin.Context) {
	tr := i18n.Get(c)
	if h.pluginManager == nil {
		h.redirectPlugins(c, "", tr.T("插件管理器未初始化"))
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	action := strings.TrimSpace(c.Param("action"))
	if id == "" {
		h.redirectPlugins(c, "", tr.T("未指定插件"))
		return
	}
	if h.pluginManager.Get(id) == nil {
		h.redirectPlugins(c, "", tr.T("插件不存在: %s", id))
		return
	}

	switch action {
	case "enable":
		wasEnabled := h.enabledPluginSet(c)[id]
		if !wasEnabled {
			if err := h.pluginManager.CallLifecycle(c, id, plugin.LifecycleActivate); err != nil {
				h.redirectPlugins(c, "", tr.T("启用插件失败: %s", err.Error()))
				return
			}
		}
		if err := h.pluginManager.Enable(c, id); err != nil {
			if !wasEnabled {
				_ = h.pluginManager.CallLifecycle(c, id, plugin.LifecycleDeactivate)
			}
			h.redirectPlugins(c, "", tr.T("启用插件失败: %s", err.Error()))
			return
		}
		if err := h.reloadPluginRuntime(c); err != nil {
			if !wasEnabled {
				_ = h.pluginManager.Disable(c, id)
				_ = h.pluginManager.CallLifecycle(c, id, plugin.LifecycleDeactivate)
				_ = h.reloadPluginRuntime(c)
			}
			h.redirectPlugins(c, "", tr.T("启用插件失败，已回滚: %s", err.Error()))
			return
		}
		h.redirectPlugins(c, tr.T("插件「%s」已启用", id), "")
	case "disable":
		wasEnabled := h.enabledPluginSet(c)[id]
		if wasEnabled {
			if err := h.pluginManager.CallLifecycle(c, id, plugin.LifecycleDeactivate); err != nil {
				h.redirectPlugins(c, "", tr.T("停用插件失败: %s", err.Error()))
				return
			}
		}
		if err := h.pluginManager.Disable(c, id); err != nil {
			h.redirectPlugins(c, "", tr.T("停用插件失败: %s", err.Error()))
			return
		}
		if err := h.reloadPluginRuntime(c); err != nil {
			if wasEnabled {
				_ = h.pluginManager.Enable(c, id)
				_ = h.pluginManager.CallLifecycle(c, id, plugin.LifecycleActivate)
				_ = h.reloadPluginRuntime(c)
			}
			h.redirectPlugins(c, "", tr.T("停用插件失败，已回滚: %s", err.Error()))
			return
		}
		h.redirectPlugins(c, tr.T("插件「%s」已停用", id), "")
	case "reload":
		if err := h.reloadPluginRuntime(c); err != nil {
			if pluginActionWantsJSON(c) {
				c.JSON(http.StatusOK, gin.H{"ok": false, "error": tr.T("重载插件失败: %s", err.Error())})
				return
			}
			h.redirectPlugins(c, "", tr.T("重载插件失败: %s", err.Error()))
			return
		}
		if pluginActionWantsJSON(c) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		h.redirectPlugins(c, tr.T("插件「%s」已重载", id), "")
	case "uninstall":
		if err := h.pluginManager.Uninstall(c, id); err != nil {
			h.redirectPlugins(c, "", tr.T("卸载插件失败: %s", err.Error()))
			return
		}
		if err := h.reloadPluginRuntime(c); err != nil {
			h.redirectPlugins(c, "", tr.T("卸载插件后重载失败: %s", err.Error()))
			return
		}
		h.redirectPlugins(c, tr.T("插件「%s」已卸载", id), "")
	default:
		h.redirectPlugins(c, "", tr.T("未知插件操作: %s", action))
	}
}

// PluginSettingsPage 渲染插件设置页。
func (h *Admin) PluginSettingsPage(c *gin.Context) {
	tr := i18n.Get(c)
	p, ok := h.pluginForRequest(c, tr)
	if !ok {
		return
	}
	data := h.base(c, tr.T("插件设置"))
	data["CurrentAdminNav"] = "plugins"
	loadErrors := map[string]string{}
	if h.pluginManager != nil {
		loadErrors = h.pluginManager.LoadErrors()
	}
	data["Plugin"] = toPluginView(tr, p, h.enabledPluginSet(c)[p.ID], loadErrors[p.ID])
	data["PluginID"] = p.ID
	data["PluginName"] = tr.D(p.PluginDomain()).T(p.Name)
	data["tp"] = tr.D(p.PluginDomain())
	if len(p.Options) == 0 {
		data["NoOptions"] = true
		c.HTML(http.StatusOK, "admin_plugin_settings.gohtml", data)
		return
	}
	options := make([]pluginOptionData, 0, len(p.Options))
	for _, opt := range p.Options {
		val, _ := h.st.GetSetting(c, plugin.OptionKey(p.ID, opt.ID))
		if val == "" {
			val = opt.Default
		}
		options = append(options, pluginOptionData{OptionDecl: opt, Value: val})
	}
	data["Options"] = options
	if msg := c.Query("notice"); msg != "" {
		data["Notice"] = msg
	}
	c.HTML(http.StatusOK, "admin_plugin_settings.gohtml", data)
}

// SavePluginSettings 保存插件全局设置。
func (h *Admin) SavePluginSettings(c *gin.Context) {
	tr := i18n.Get(c)
	p, ok := h.pluginForRequest(c, tr)
	if !ok {
		return
	}
	for _, opt := range p.Options {
		val := c.PostForm("option_" + opt.ID)
		if opt.Type == "bool" && val == "" {
			val = "false"
		}
		key := plugin.OptionKey(p.ID, opt.ID)
		if val == "" || val == opt.Default {
			_ = h.st.DeleteSetting(c, key)
			continue
		}
		if err := h.st.SaveSetting(c, key, val); err != nil {
			h.serverError(c, err)
			return
		}
	}
	if h.enabledPluginSet(c)[p.ID] {
		if err := h.reloadPluginRuntime(c); err != nil {
			h.redirectPlugins(c, "", tr.T("插件设置已保存，但重载失败: %s", err.Error()))
			return
		}
	}
	c.Redirect(http.StatusSeeOther, "/admin/plugin/"+url.PathEscape(p.ID)+"/settings?notice="+url.QueryEscape(tr.T("插件设置已保存")))
}

func (h *Admin) reloadPluginRuntime(c *gin.Context) error {
	if h.pluginManager == nil {
		return nil
	}
	if err := h.pluginManager.LoadEnabledFunctions(c); err != nil {
		return err
	}
	// 重建组件注册表：插件启用/停用后，可用组件列表发生变化。
	h.rebuildWidgetRegistry(c)
	// SetHookRegistry 已在 init 时传入了 getter，LoadTheme 内部会通过 getter 拿到新 Registry。
	if h.themeManager != nil {
		if err := h.themeManager.LoadTheme(c, ""); err != nil {
			return err
		}
	}
	return nil
}

// rebuildWidgetRegistry 重建统一组件注册表并注入 renderer。
// 插件启用/停用后需要调用，确保组件注册表与当前启用插件列表一致。
func (h *Admin) rebuildWidgetRegistry(ctx context.Context) {
	if h.renderer == nil {
		return
	}
	widgetRegistry := hook.NewWidgetRegistry()
	theme.RegisterBuiltins(widgetRegistry)
	if h.themeManager != nil {
		if t := h.themeManager.Current(ctx); t != nil {
			theme.RegisterThemeWidgets(widgetRegistry, t)
		}
	}
	if h.pluginManager != nil {
		h.pluginManager.RegisterPluginWidgets(ctx, widgetRegistry)
	}
	h.renderer.SetWidgetResolver(widgetRegistry.Get)
}

func (h *Admin) pluginForRequest(c *gin.Context, tr *gettext.Translations) (*plugin.Plugin, bool) {
	if h.pluginManager == nil {
		h.redirectPlugins(c, "", tr.T("插件管理器未初始化"))
		return nil, false
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		h.redirectPlugins(c, "", tr.T("未指定插件"))
		return nil, false
	}
	p := h.pluginManager.Get(id)
	if p == nil {
		h.redirectPlugins(c, "", tr.T("插件不存在: %s", id))
		return nil, false
	}
	return p, true
}

func (h *Admin) enabledPluginSet(c *gin.Context) map[string]bool {
	set := make(map[string]bool)
	if h.pluginManager == nil {
		return set
	}
	for _, id := range h.pluginManager.EnabledIDs(c) {
		set[id] = true
	}
	return set
}

func translatePluginList(tr *gettext.Translations, plugins []*plugin.Plugin, enabled map[string]bool, loadErrors map[string]string) []*pluginView {
	result := make([]*pluginView, 0, len(plugins))
	for _, p := range plugins {
		if p == nil {
			continue
		}
		result = append(result, toPluginView(tr, p, enabled[p.ID], loadErrors[p.ID]))
	}
	return result
}

func toPluginView(tr *gettext.Translations, p *plugin.Plugin, enabled bool, loadError string) *pluginView {
	domain := tr.D(p.PluginDomain())
	name := p.Name
	if name != "" {
		if translated := domain.T(name); translated != "" {
			name = translated
		}
	}
	desc := p.Description
	if desc != "" {
		if translated := domain.T(desc); translated != "" {
			desc = translated
		}
	}
	return &pluginView{
		Plugin:       p,
		Name:         name,
		Description:  desc,
		Enabled:      enabled,
		HasAssets:    p.HasAssets(),
		LoadError:    loadError,
		ActionNames:  append([]string(nil), p.Hooks.Actions...),
		FilterNames:  append([]string(nil), p.Hooks.Filters...),
		SlotNames:    append([]string(nil), p.Hooks.Slots...),
		WidgetNames:  pluginWidgetNames(p.Widgets),
		OptionNames: pluginOptionNames(p.Options),
		Source:       "plugin:" + p.ID,
	}
}

func pluginWidgetNames(widgets []plugin.WidgetDecl) []string {
	out := make([]string, 0, len(widgets))
	for _, w := range widgets {
		if w.ID != "" {
			out = append(out, w.ID)
		}
	}
	return out
}

func pluginOptionNames(options []plugin.OptionDecl) []string {
	out := make([]string, 0, len(options))
	for _, opt := range options {
		if opt.ID != "" {
			out = append(out, opt.ID)
		}
	}
	return out
}

func (h *Admin) redirectPlugins(c *gin.Context, message, errMsg string) {
	values := url.Values{}
	if message != "" {
		values.Set("message", message)
	}
	if errMsg != "" {
		values.Set("error", errMsg)
	}
	path := "/admin/plugins"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	c.Redirect(http.StatusSeeOther, path)
}

func pluginActionWantsJSON(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "application/json") || c.GetHeader("X-Requested-With") == "fetch"
}

// PluginUpload 处理插件 zip 上传。
func (h *Admin) PluginUpload(c *gin.Context) {
	tr := i18n.Get(c)
	fh, err := c.FormFile("plugin_zip")
	if err != nil {
		h.redirectPlugins(c, "", tr.T("未选择文件"))
		return
	}
	if fh.Size > maxPluginUploadSize {
		h.redirectPlugins(c, "", tr.T("插件包不能超过 5MB"))
		return
	}
	if !strings.HasSuffix(strings.ToLower(fh.Filename), ".zip") {
		h.redirectPlugins(c, "", tr.T("仅支持 .zip 格式的插件包"))
		return
	}

	file, err := fh.Open()
	if err != nil {
		h.redirectPlugins(c, "", tr.T("打开上传文件失败"))
		return
	}
	defer file.Close()

	tmpDir, err := os.MkdirTemp("", "plugin-upload-*")
	if err != nil {
		h.redirectPlugins(c, "", tr.T("创建临时目录失败"))
		return
	}
	defer os.RemoveAll(tmpDir)

	if err := extractPluginZip(file, fh.Size, tmpDir); err != nil {
		h.redirectPlugins(c, "", tr.T("解压插件包失败: %s", err.Error()))
		return
	}

	pluginRoot, err := findPluginRoot(tmpDir)
	if err != nil {
		h.redirectPlugins(c, "", tr.T("插件包中未找到 plugin.yaml"))
		return
	}

	pm := h.pluginManager
	if pm == nil {
		h.redirectPlugins(c, "", tr.T("插件管理器未初始化"))
		return
	}
	installed, err := pm.Install(pluginRoot)
	if err != nil {
		h.redirectPlugins(c, "", tr.T("安装插件失败: %s", err.Error()))
		return
	}

	h.redirectPlugins(c, tr.T("插件「%s」安装成功", installed.ID), "")
}

// PluginDownload 下载指定插件为 zip 包。
func (h *Admin) PluginDownload(c *gin.Context) {
	tr := i18n.Get(c)
	id := strings.TrimSpace(c.Query("plugin"))
	if id == "" {
		h.redirectPlugins(c, "", tr.T("未指定插件 ID"))
		return
	}
	pm := h.pluginManager
	if pm == nil {
		h.redirectPlugins(c, "", tr.T("插件管理器未初始化"))
		return
	}
	p := pm.Get(id)
	if p == nil {
		h.redirectPlugins(c, "", tr.T("插件「%s」不存在", id))
		return
	}
	tmp, err := os.CreateTemp("", "plugin-download-*.zip")
	if err != nil {
		h.redirectPlugins(c, "", tr.T("创建插件下载文件失败"))
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := writePluginZip(tmp, p.ID, p.Dir); err != nil {
		_ = tmp.Close()
		h.redirectPlugins(c, "", tr.T("打包插件失败: %s", err.Error()))
		return
	}
	if err := tmp.Close(); err != nil {
		h.redirectPlugins(c, "", tr.T("写入插件下载文件失败: %s", err.Error()))
		return
	}
	c.FileAttachment(tmpName, p.ID+".zip")
}

func writePluginZip(w io.Writer, rootName, dir string) error {
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

// extractPluginZip 解压 zip 到目标目录，带路径穿越防护。
func extractPluginZip(r io.ReaderAt, size int64, dest string) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	var total int64
	var files int
	for _, f := range zr.File {
		name := filepath.Clean(f.Name)
		if !safePluginZipPath(name) {
			continue
		}
		if len(filepath.Base(name)) > maxPluginExtractedName {
			return fmt.Errorf("plugin file name too long: %s", name)
		}
		target := filepath.Join(dest, name)
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
		if files > maxPluginExtractedFiles {
			return fmt.Errorf("plugin package contains too many files")
		}
		headerSize := int64(f.UncompressedSize64)
		if headerSize > maxPluginExtractedFile {
			return fmt.Errorf("plugin file %s is too large", name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if !isAllowedPluginFile(name) {
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
		written, err := copyPluginZipFile(out, rc)
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
		if total > maxPluginExtractedSize {
			_ = os.Remove(target)
			return fmt.Errorf("plugin package uncompressed size is too large")
		}
	}
	return nil
}

func copyPluginZipFile(out io.Writer, rc io.Reader) (int64, error) {
	lr := &io.LimitedReader{R: rc, N: maxPluginExtractedFile + 1}
	written, err := io.Copy(out, lr)
	if err != nil {
		return written, err
	}
	if written > maxPluginExtractedFile {
		return written, fmt.Errorf("plugin file is too large")
	}
	return written, nil
}

func safePluginZipPath(name string) bool {
	if name == "." || name == ".." || filepath.IsAbs(name) {
		return false
	}
	return !strings.HasPrefix(name, ".."+string(filepath.Separator))
}

// isAllowedPluginFile 检查文件扩展名是否在插件文件白名单中。
func isAllowedPluginFile(name string) bool {
	return isAllowedThemeFile(name)
}

// findPluginRoot 在解压目录中查找 plugin.yaml 所在目录。
func findPluginRoot(dir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, "plugin.yaml")); err == nil {
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sub := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(filepath.Join(sub, "plugin.yaml")); err == nil {
			return sub, nil
		}
	}
	return "", fmt.Errorf("plugin.yaml not found in %s", dir)
}
