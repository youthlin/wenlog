package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/i18n"
)

const defaultBackupKeep = 10

// BackupPage 备份管理页面。
func (h *Admin) BackupPage(c *gin.Context) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("数据库备份"))
	data["CurrentAdminNav"] = "backup"

	backups, err := h.st.ListBackups()
	if err != nil && h.log != nil {
		h.log.Error("list backups", "error", err)
	}
	data["Backups"] = backups
	data["DBPath"] = h.st.DBPath()
	h.injectBackupSettings(c, data)

	c.HTML(http.StatusOK, "admin_backup.gohtml", data)
}

// BackupNow 手动创建备份。
func (h *Admin) BackupNow(c *gin.Context) {
	tr := i18n.Get(c)
	path, err := h.st.BackupDB()
	if err != nil {
		h.backupError(c, tr.T("备份失败: %v", err))
		return
	}
	// 清理旧备份
	_ = h.st.CleanOldBackups(defaultBackupKeep)
	h.backupSuccess(c, tr.T("备份成功: %s", filepath.Base(path)))
}

// BackupEmail 创建备份并通过邮件发送。
func (h *Admin) BackupEmail(c *gin.Context) {
	tr := i18n.Get(c)
	emailTo := strings.TrimSpace(c.PostForm("email_to"))
	if emailTo == "" {
		h.backupError(c, tr.T("请输入收件人邮箱"))
		return
	}

	// 创建备份
	path, err := h.st.BackupDB()
	if err != nil {
		h.backupError(c, tr.T("备份失败: %v", err))
		return
	}
	defer func() {
		// 发送后删除临时备份文件（邮件已发送，备份文件可清理）
		_ = os.Remove(path)
	}()
	_ = h.st.CleanOldBackups(defaultBackupKeep)

	// 读取备份文件
	data, err := os.ReadFile(path)
	if err != nil {
		h.backupError(c, tr.T("读取备份文件失败: %v", err))
		return
	}

	// 获取 SMTP 配置
	smtpCfg := smtpConfigFromStore(c, h.st)
	if !smtpCfg.Configured() {
		h.backupError(c, tr.T("SMTP 未配置，请先在设置中配置邮件服务"))
		return
	}

	siteName := siteNameFromStore(c, h.st)
	siteURL := siteURLFromRequest(h.st, c)
	subject := tr.T("%s - 数据库备份 %s", siteName, time.Now().Format("2006-01-02 15:04"))
	body := tr.T("这是 %s 的数据库备份文件，备份时间：%s。", siteName, time.Now().Format("2006-01-02 15:04:05"))
	body = mailBodyWithSiteDomain(tr, body, siteURL)

	if err := smtpCfg.SendWithAttachment(emailTo, subject, body, filepath.Base(path), data); err != nil {
		h.backupError(c, tr.T("邮件发送失败: %v", err))
		return
	}

	h.backupSuccess(c, tr.T("备份已发送到 %s", emailTo))
}

// SaveBackupSettings 保存自动备份设置。
func (h *Admin) SaveBackupSettings(c *gin.Context) {
	tr := i18n.Get(c)
	enabled := c.PostForm("auto_backup_enabled") == "on"
	backupTime := strings.TrimSpace(c.PostForm("auto_backup_time"))
	if !validBackupTime(backupTime) {
		h.backupError(c, tr.T("自动备份时间不合法"))
		return
	}
	keep := positiveIntSetting(c.PostForm("auto_backup_keep"), consts.SettingsAutoBackupKeepDefault)
	settings := map[string]string{
		consts.SettingsAutoBackupEnabled: strconv.FormatBool(enabled),
		consts.SettingsAutoBackupTime:    backupTime,
		consts.SettingsAutoBackupKeep:    strconv.Itoa(keep),
	}
	for key, value := range settings {
		if err := h.st.SetSetting(c, key, value); err != nil {
			h.serverError(c, err)
			return
		}
	}
	h.backupSuccess(c, tr.T("自动备份设置已保存"))
}

// BackupRestore 从备份文件恢复数据库。
func (h *Admin) BackupRestore(c *gin.Context) {
	tr := i18n.Get(c)
	filename := strings.TrimSpace(c.PostForm("filename"))
	if filename == "" {
		h.backupError(c, tr.T("请选择要恢复的备份文件"))
		return
	}

	// 安全检查
	name := filepath.Base(filename)
	if !strings.HasSuffix(name, ".db") {
		h.backupError(c, tr.T("无效的备份文件名"))
		return
	}

	backupPath := filepath.Join(filepath.Dir(h.st.DBPath()), "backups", name)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		h.backupError(c, tr.T("备份文件不存在"))
		return
	}

	if err := h.st.RestoreDB(backupPath); err != nil {
		h.backupError(c, tr.T("恢复失败: %v", err))
		return
	}

	h.backupSuccess(c, tr.T("数据库已从 %s 恢复成功", name))
}

// BackupDelete 删除指定备份文件。
func (h *Admin) BackupDelete(c *gin.Context) {
	tr := i18n.Get(c)
	filename := strings.TrimSpace(c.PostForm("filename"))
	if filename == "" {
		h.backupError(c, tr.T("请指定要删除的备份文件"))
		return
	}

	if err := h.st.DeleteBackup(filename); err != nil {
		h.backupError(c, tr.T("删除失败: %v", err))
		return
	}

	h.backupSuccess(c, tr.T("备份文件 %s 已删除", filename))
}

func (h *Admin) backupError(c *gin.Context, msg string) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("数据库备份"))
	data["CurrentAdminNav"] = "backup"
	data["Error"] = msg
	data["DBPath"] = h.st.DBPath()
	backups, _ := h.st.ListBackups()
	data["Backups"] = backups
	h.injectBackupSettings(c, data)
	c.HTML(http.StatusOK, "admin_backup.gohtml", data)
}

func (h *Admin) backupSuccess(c *gin.Context, msg string) {
	tr := i18n.Get(c)
	data := h.base(c, tr.T("数据库备份"))
	data["CurrentAdminNav"] = "backup"
	data["Notice"] = msg
	data["DBPath"] = h.st.DBPath()
	backups, _ := h.st.ListBackups()
	data["Backups"] = backups
	h.injectBackupSettings(c, data)
	c.HTML(http.StatusOK, "admin_backup.gohtml", data)
}

func (h *Admin) injectBackupSettings(c *gin.Context, data gin.H) {
	settings, err := h.st.GetSettings(c, consts.SettingsAutoBackupEnabled, consts.SettingsAutoBackupTime, consts.SettingsAutoBackupKeep)
	if err != nil && h.log != nil {
		h.log.Error("get backup settings", "error", err)
	}
	enabled := true
	if strings.EqualFold(settings[consts.SettingsAutoBackupEnabled], "false") {
		enabled = false
	}
	backupTime := strings.TrimSpace(settings[consts.SettingsAutoBackupTime])
	if !validBackupTime(backupTime) {
		backupTime = consts.SettingsAutoBackupTimeDefault
	}
	keep := positiveIntSetting(settings[consts.SettingsAutoBackupKeep], consts.SettingsAutoBackupKeepDefault)
	data["AutoBackupEnabled"] = enabled
	data["AutoBackupTime"] = backupTime
	data["AutoBackupKeep"] = keep
}

func validBackupTime(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return false
	}
	minute, err := strconv.Atoi(parts[1])
	return err == nil && minute >= 0 && minute <= 59
}
