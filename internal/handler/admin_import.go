package handler

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/i18n"
	wpimport "github.com/youthlin/blog/internal/importer"
	"github.com/youthlin/blog/internal/util"
)

const maxImportXMLSize = 50 << 20

const (
	importXMLDataSessionKey       = "import_xml_data"
	importXMLPathSessionKey       = "import_xml_path"
	importDefaultUserIDSessionKey = "import_default_user_id"
	importFileNameSessionKey      = "import_file_name"
)

// ImportPage 显示 WXR 导入/导出页。
func (h *Admin) ImportPage(c *gin.Context) {
	tr := i18n.Get(c)
	data, ok := h.importPageData(c, tr.T("WXR 导入 / 导出"))
	if !ok {
		return
	}
	c.HTML(http.StatusOK, "admin_import.gohtml", data)
}

// ImportXML 处理后台上传的 WXR XML。
// 两步流程: 1) 上传 XML 预览作者映射; 2) 确认映射后执行导入。
func (h *Admin) ImportXML(c *gin.Context) {
	tr := i18n.Get(c)
	data, ok := h.importPageData(c, tr.T("WXR 导入 / 导出"))
	if !ok {
		return
	}

	// 第二步: 确认作者映射并执行导入
	if c.PostForm("confirm") == "1" {
		h.importXMLConfirm(c, data)
		return
	}

	// 第一步: 上传 XML 预览作者
	var form struct {
		UserID uint `form:"user_id"`
	}
	if err := c.ShouldBind(&form); err != nil {
		data["Error"] = tr.T("表单参数有误。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	data["SelectedUserID"] = form.UserID
	if form.UserID == 0 {
		data["Error"] = tr.T("请选择默认归属用户。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	_, err := h.st.GetUserByID(c, form.UserID)
	if err != nil {
		data["Error"] = tr.T("所选用户不存在。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	fh, err := c.FormFile("xml_file")
	if err != nil {
		data["Error"] = tr.T("请选择要导入的 XML 文件。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	if fh.Size <= 0 {
		data["Error"] = tr.T("XML 文件不能为空。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	if fh.Size > maxImportXMLSize {
		data["Error"] = tr.T("XML 文件不能超过 50MB。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	file, err := fh.Open()
	if err != nil {
		data["Error"] = tr.T("打开上传文件失败。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	defer file.Close()

	// 读取 XML 内容到内存(用于后续导入)
	xmlBytes, err := io.ReadAll(file)
	if err != nil {
		data["Error"] = tr.T("读取上传文件失败。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}

	// 提取 XML 中的作者列表
	authors, err := wpimport.PreviewAuthors(bytes.NewReader(xmlBytes))
	if err != nil {
		data["Error"] = tr.T("解析 XML 失败: %s", err.Error())
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}

	// 将 XML 内容暂存到临时文件,session 中只保存路径,避免大文件撑爆 Cookie session。
	if err := saveImportXMLPreview(c, xmlBytes, form.UserID, fh.Filename); err != nil {
		data["Error"] = tr.T("保存会话失败。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}

	data["ImportAuthors"] = authors
	data["ImportFileName"] = fh.Filename
	data["ImportDefaultUserID"] = form.UserID
	c.HTML(http.StatusOK, "admin_import.gohtml", data)
}

// importXMLConfirm 第二步: 确认作者映射后执行导入。
func (h *Admin) importXMLConfirm(c *gin.Context, data gin.H) {
	tr := i18n.Get(c)
	s := sessions.Default(c)

	xmlBytes, defaultUserID, fileName, xmlPath, ok := importXMLPreviewFromSession(s)
	if !ok || len(xmlBytes) == 0 {
		clearImportXMLPreview(s, xmlPath)
		data["Error"] = tr.T("会话已过期，请重新上传 XML 文件。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}

	// 清除 session 中的暂存数据
	clearImportXMLPreview(s, xmlPath)

	// 解析作者映射: author_<name> = user_id
	authorMapping := make(map[string]uint)
	for key := range c.Request.PostForm {
		if strings.HasPrefix(key, "author_") {
			authorName := strings.TrimPrefix(key, "author_")
			userIDStr := c.PostForm(key)
			if uid, err := strconv.ParseUint(userIDStr, 10, 64); err == nil && uid > 0 {
				authorMapping[authorName] = uint(uid)
			}
		}
	}

	stats, err := wpimport.ImportReader(h.st.DB(), bytes.NewReader(xmlBytes), wpimport.Options{
		TargetUserID:  defaultUserID,
		IncludeDrafts: true,
		AuthorMapping: authorMapping,
	})
	if err != nil {
		h.log.Error("import xml",
			slog.Any("error", err),
			slog.String("file", fileName),
		)
		data["Error"] = tr.T("导入失败: %s", err.Error())
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	data["Success"] = tr.T("导入完成，若 XML 中存在相同 ID 的文章/页面/评论，已按 upsert 覆盖保存。")
	data["ImportStats"] = stats
	data["ImportedFileName"] = fileName
	c.HTML(http.StatusOK, "admin_import.gohtml", data)
}

func saveImportXMLPreview(c *gin.Context, xmlBytes []byte, defaultUserID uint, fileName string) error {
	s := sessions.Default(c)
	if oldPath, _ := s.Get(importXMLPathSessionKey).(string); oldPath != "" {
		_ = os.Remove(oldPath)
	}
	tmp, err := os.CreateTemp("", "blog-import-*.xml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(xmlBytes); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	s.Delete(importXMLDataSessionKey)
	s.Set(importXMLPathSessionKey, tmpPath)
	s.Set(importDefaultUserIDSessionKey, defaultUserID)
	s.Set(importFileNameSessionKey, fileName)
	if err := s.Save(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func importXMLPreviewFromSession(s sessions.Session) ([]byte, uint, string, string, bool) {
	defaultUserID, _ := s.Get(importDefaultUserIDSessionKey).(uint)
	fileName, _ := s.Get(importFileNameSessionKey).(string)
	if xmlPath, _ := s.Get(importXMLPathSessionKey).(string); strings.TrimSpace(xmlPath) != "" {
		xmlBytes, err := os.ReadFile(xmlPath)
		return xmlBytes, defaultUserID, fileName, xmlPath, err == nil && len(xmlBytes) > 0
	}
	// 兼容旧版本已写入 session 的预览数据,下一步确认时会被清理掉。
	if xmlBytes, ok := s.Get(importXMLDataSessionKey).([]byte); ok {
		return xmlBytes, defaultUserID, fileName, "", len(xmlBytes) > 0
	}
	return nil, defaultUserID, fileName, "", false
}

func clearImportXMLPreview(s sessions.Session, xmlPath string) {
	if strings.TrimSpace(xmlPath) != "" {
		_ = os.Remove(xmlPath)
	}
	s.Delete(importXMLDataSessionKey)
	s.Delete(importXMLPathSessionKey)
	s.Delete(importDefaultUserIDSessionKey)
	s.Delete(importFileNameSessionKey)
	_ = s.Save()
}

// ExportXML 导出可被当前后台重新导入的 XML。
func (h *Admin) ExportXML(c *gin.Context) {
	tr := i18n.Get(c)
	data, ok := h.importPageData(c, tr.T("WXR 导入 / 导出"))
	if !ok {
		return
	}
	var form struct {
		Posts    []string `form:"include_posts"`
		Pages    []string `form:"include_pages"`
		Comments []string `form:"include_comments"`
		Settings []string `form:"include_settings"`
	}
	if err := c.ShouldBind(&form); err != nil {
		data["Error"] = tr.T("导出表单参数有误。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	includePosts := len(form.Posts) > 0
	includePages := len(form.Pages) > 0
	includeComments := len(form.Comments) > 0
	includeSettings := len(form.Settings) > 0
	data["ExportPosts"] = includePosts
	data["ExportPages"] = includePages
	data["ExportComments"] = includeComments
	data["ExportSettings"] = includeSettings
	if !includePosts && !includePages && !includeComments && !includeSettings {
		data["Error"] = tr.T("请至少选择一项导出内容。")
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	v, err := h.st.GetSetting(c, consts.SettingsSiteName)
	if err != nil && h.log != nil {
		h.log.Error("get site name for export", "error", err)
	}
	xmlData, _, err := wpimport.ExportXML(h.st.DB(), wpimport.ExportOptions{
		Posts:     includePosts,
		Pages:     includePages,
		Comments:  includeComments,
		Settings:  includeSettings,
		SiteTitle: util.FirstNonEmptyOr(consts.SettingsSiteNameDefault, v),
		SiteURL:   requestBaseURL(c),
	})
	if err != nil {
		data["Error"] = tr.T("导出失败: %s", err.Error())
		c.HTML(http.StatusBadRequest, "admin_import.gohtml", data)
		return
	}
	c.Header("Content-Type", "application/xml; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+wpimport.ExportFilename()+`"`)
	c.Data(http.StatusOK, "application/xml; charset=utf-8", xmlData)
}
func (h *Admin) importPageData(c *gin.Context, title string) (gin.H, bool) {
	data := h.base(c, title)
	data["SelectedUserID"] = uint(0)
	data["ExportPosts"] = true
	data["ExportPages"] = true
	data["ExportComments"] = true
	data["ExportSettings"] = true
	users, err := h.st.ListUsers(c)
	if err != nil {
		h.serverError(c, err)
		return nil, false
	}
	data["Users"] = users
	return data, true
}
