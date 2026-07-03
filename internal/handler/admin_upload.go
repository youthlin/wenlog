package handler

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/i18n"
	"github.com/youthlin/blog/internal/imageutil"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/util"
)

func (h *Admin) UploadFile(c *gin.Context) {
	tr := i18n.Get(c)
	u := h.currentUser(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "message": tr.T("未登录")})
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": tr.T("未选择文件")})
		return
	}
	if fh.Size > consts.MaxUploadSize {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": tr.T("文件不能超过 10MB")})
		return
	}
	file, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": tr.T("打开上传文件失败")})
		return
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, _ := io.ReadFull(file, buf)
	buf = buf[:n]
	mimeType := http.DetectContentType(buf)
	if !allowedUploadMIME(mimeType) {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": tr.T("仅支持 png/jpg/gif/webp 图片")})
		return
	}
	if _, err = file.Seek(0, 0); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": tr.T("读取上传文件失败")})
		return
	}

	now := time.Now()
	ext := safeImageExt(fh.Filename, mimeType)
	relDir := filepath.Join("wp-content", "uploads", now.Format("2006"), now.Format("01"))
	absDir := filepath.Join(h.cfg.PublicDir, relDir)
	if err = os.MkdirAll(absDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": tr.T("创建上传目录失败")})
		return
	}

	// 尽量保留原始文件名，做安全清理后使用；冲突时追加序号
	baseName := sanitizeFilename(strings.TrimSuffix(fh.Filename, ext))
	fileName := baseName + ext
	if _, err := os.Stat(filepath.Join(absDir, fileName)); err == nil {
		for i := 1; i < 100; i++ {
			candidate := fmt.Sprintf("%s-%d%s", baseName, i, ext)
			if _, err := os.Stat(filepath.Join(absDir, candidate)); os.IsNotExist(err) {
				fileName = candidate
				break
			}
		}
		// 极端情况：100 个同名文件都冲突，回退到随机名
		if fileName == baseName+ext {
			fileName = util.GenerateRandomString(consts.TokenLengthUpload, util.WithAlphaNumer()) + ext
		}
	}
	absPath := filepath.Join(absDir, fileName)
	out, err := os.Create(absPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": tr.T("保存文件失败")})
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		_ = os.Remove(absPath)
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": tr.T("写入文件失败")})
		return
	}
	_ = out.Close()

	width, height := imageSize(absPath)
	urlPath := "/" + filepath.ToSlash(filepath.Join(relDir, fileName))
	record := &model.Upload{Path: urlPath, OrigName: fh.Filename, MimeType: mimeType, Size: fh.Size, Width: width, Height: height, UploaderID: u.ID, CreatedAt: now}

	// 生成缩略图
	thumbs, err := imageutil.GenerateThumbnails(absPath, urlPath)
	if err != nil && h.log != nil {
		h.log.Warn("generate thumbnails", slog.String("path", absPath), slog.Any("error", err))
	}
	for _, t := range thumbs {
		switch t.Width {
		case 150:
			record.Thumb150Path = t.URL
		case 300:
			record.Thumb300Path = t.URL
		case 768:
			record.Thumb768Path = t.URL
		}
	}

	if err := h.st.SaveUpload(c, record); err != nil {
		_ = os.Remove(absPath)
		imageutil.RemoveThumbnails(absPath)
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": tr.T("保存上传记录失败")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": record.ID, "url": urlPath, "markdown": "![](" + urlPath + ")"})
}

// UploadsPage 文件管理页。
func (h *Admin) UploadsPage(c *gin.Context) {
	tr := i18n.Get(c)
	page := atoiDefault(c.Query("page"), 1)
	var uploaderID uint
	if h.currentUserRole(c) == model.RoleAuthor {
		uploaderID = currentUserID(c)
	}
	uploads, total, err := h.st.ListUploadsForUser(c, page, adminPageSize, uploaderID)
	if err != nil {
		h.serverError(c, err)
		return
	}
	data := h.base(c, tr.T("文件管理"))
	data["Uploads"] = uploads
	data["Total"] = total
	data["Page"] = page
	data["Pages"] = int((total + int64(adminPageSize) - 1) / int64(adminPageSize))
	c.HTML(http.StatusOK, "admin_uploads.gohtml", data)
}

// UploadsJSON 返回最近上传文件的 JSON 列表,供编辑器文件选择器使用。
func (h *Admin) UploadsJSON(c *gin.Context) {
	page := atoiDefault(c.Query("page"), 1)
	var uploaderID uint
	if h.currentUserRole(c) == model.RoleAuthor {
		uploaderID = currentUserID(c)
	}
	uploads, _, err := h.st.ListUploadsForUser(c, page, adminPageSize, uploaderID)
	if err != nil {
		h.log.ErrorContext(c, "查询上传文件失败", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": "查询上传文件失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "uploads": uploads})
}

// DeleteUpload 删除上传文件及元数据。
func (h *Admin) DeleteUpload(c *gin.Context) {
	id, err := parseUintParam(c.Param("id"))
	if err != nil {
		h.notFound(c)
		return
	}
	u, err := h.st.GetUpload(c, id)
	if err != nil {
		h.notFound(c)
		return
	}
	if h.currentUserRole(c) != model.RoleAdmin && u.UploaderID != currentUserID(c) {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	absPath := filepath.Join(h.cfg.PublicDir, strings.TrimPrefix(filepath.FromSlash(u.Path), string(filepath.Separator)))
	_ = os.Remove(absPath)
	imageutil.RemoveThumbnails(absPath)
	if err := h.st.DeleteUpload(c, id); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/uploads")
}

func allowedUploadMIME(m string) bool {
	switch m {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func safeImageExt(name, mimeType string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch mimeType {
	case "image/png":
		if ext != ".png" {
			return ".png"
		}
	case "image/jpeg":
		if ext != ".jpg" && ext != ".jpeg" {
			return ".jpg"
		}
	case "image/gif":
		if ext != ".gif" {
			return ".gif"
		}
	case "image/webp":
		if ext != ".webp" {
			return ".webp"
		}
	}
	if ext == "" {
		return ".bin"
	}
	return ext
}

func imageSize(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// sanitizeFilename 清理文件名，移除路径分隔符、特殊字符，空格转连字符，转小写，限制长度。
func sanitizeFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r == ' ' || r == '_':
			b.WriteByte('-')
		case r == '.' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			// 跳过其他字符
		}
	}
	result := b.String()
	// 去除首尾连字符
	result = strings.Trim(result, "-")
	// 限制长度
	if len(result) > 100 {
		result = result[:100]
	}
	if result == "" {
		return "file"
	}
	return result
}
