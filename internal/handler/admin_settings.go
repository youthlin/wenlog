package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	gettext "github.com/youthlin/t"

	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/i18n"
	"github.com/youthlin/wenlog/internal/middleware"
	"github.com/youthlin/wenlog/internal/permalink"
	"github.com/youthlin/wenlog/internal/util"
	"github.com/youthlin/wenlog/internal/version"
	"github.com/youthlin/wenlog/web"
)

const (
	githubLatestReleaseURL = "https://github.com/youthlin/wenlog/releases/latest"
	githubReleaseBaseURL   = "https://github.com/youthlin/wenlog/releases/download"
	giteeReleaseBaseURL    = "https://gitee.com/youthlin/wenlog/releases/download"
)

var goProxyLatestURLs = []string{
	"https://goproxy.cn/github.com/youthlin/wenlog/@latest",
	"https://proxy.golang.org/github.com/youthlin/wenlog/@latest",
}

func settingsPageURL(section string) string {
	switch normalizeSettingsSection(section) {
	case "developer":
		return "/admin/settings/developer"
	default:
		return "/admin/settings"
	}
}

func normalizeSettingsSection(section string) string {
	switch strings.TrimSpace(section) {
	case "resources", "developer":
		return "developer"
	default:
		return "general"
	}
}

// SettingsPage 后台设置页:站点设置。
func (h *Admin) SettingsPage(c *gin.Context) {
	data := h.settingsDataForSection(c, "general")
	c.HTML(http.StatusOK, "admin_settings.gohtml", data)
}

// DeveloperSettingsPage 后台开发设置页。
func (h *Admin) DeveloperSettingsPage(c *gin.Context) {
	data := h.settingsDataForSection(c, "developer")
	c.HTML(http.StatusOK, "admin_settings.gohtml", data)
}
func settingsRedirectURL(section, message string) string {
	base := settingsPageURL(normalizeSettingsSection(section))
	v := url.Values{}
	if strings.TrimSpace(message) != "" {
		v.Set("message", message)
	}
	if encoded := v.Encode(); encoded != "" {
		return base + "?" + encoded
	}
	return base
}

func (h *Admin) settingsDataForSection(c *gin.Context, section string) gin.H {
	tr := i18n.Get(c)
	currentSection := normalizeSettingsSection(section)
	title := tr.T("常规设置")
	switch currentSection {
	case "developer":
		title = tr.T("开发设置")
	}
	data := h.base(c, title)
	data["CurrentSettingsSection"] = currentSection
	settings, err := h.st.GetSettings(c, consts.SettingsSiteName,
		consts.SettingsSiteDesc,
		consts.SettingsSiteLogo,
		consts.SettingsPostPermalink,
		consts.SettingsCategoryPrefix,
		consts.SettingsTagPrefix,
		consts.SettingsPageSize,
		consts.SettingsFeedSize,
		consts.SettingsDefaultAvatar,
		consts.SettingsSessionSecret,
		consts.SettingsRegistrationOpen,
		consts.SettingsSMTPHost,
		consts.SettingsSMTPPort,
		consts.SettingsSMTPUser,
		consts.SettingsSMTPPassword,
		consts.SettingsSMTPFrom,
		consts.SettingsSiteURL,
		consts.SettingsMetricsAuthPassword,
		consts.SettingsShowSQLDetails,
		consts.SettingsUpdateDownloadMirror,
		consts.SettingsLogLevel,
	)
	if err != nil && h.log != nil {
		h.log.ErrorContext(c, "get settings for settings page", "error", err)
	}
	data["SiteNameValue"] = util.FirstNonEmptyOr(consts.SettingsSiteNameDefault, settings[consts.SettingsSiteName])
	data["SiteDescriptionValue"] = settings[consts.SettingsSiteDesc]
	data["SiteLogoValue"] = settings[consts.SettingsSiteLogo]
	data["PostPermalinkValue"] = util.FirstNonEmptyOr(consts.SettingsPostPermalinkDefault, settings[consts.SettingsPostPermalink])
	data["CategoryPrefixValue"] = util.FirstNonEmptyOr(consts.SettingsCategoryPrefixDefault, settings[consts.SettingsCategoryPrefix])
	data["TagPrefixValue"] = util.FirstNonEmptyOr(consts.SettingsTagPrefixDefault, settings[consts.SettingsTagPrefix])
	data["PageSizeValue"] = positiveIntSetting(settings[consts.SettingsPageSize], defaultPublicPageSize)
	data["FeedSizeValue"] = positiveIntSetting(settings[consts.SettingsFeedSize], defaultFeedSize)
	data["DefaultAvatarValue"] = util.NormalizeDefaultAvatar(settings[consts.SettingsDefaultAvatar])
	data["RegistrationOpenValue"] = settings[consts.SettingsRegistrationOpen] == "true"
	data["SMTPHostValue"] = settings[consts.SettingsSMTPHost]
	data["SMTPPortValue"] = settings[consts.SettingsSMTPPort]
	data["SMTPUserValue"] = settings[consts.SettingsSMTPUser]
	data["SMTPPasswordValue"] = settings[consts.SettingsSMTPPassword]
	data["SMTPFromValue"] = settings[consts.SettingsSMTPFrom]
	data["SiteURLValue"] = settings[consts.SettingsSiteURL]
	data["MetricsAuthUsernameValue"] = "metrics"
	data["MetricsAuthPasswordValue"] = h.ensureMetricsAuthPassword(c, settings[consts.SettingsMetricsAuthPassword])
	data["SMTPConfigured"] = smtpConfigFromSettings(settings).Configured()
	if strings.TrimSpace(settings[consts.SettingsSessionSecret]) != "" {
		data["SessionSecretConfigured"] = true
	}
	data["TemplateHotReload"] = h.renderer != nil && h.renderer.Hot()
	data["AssetHotReload"] = h.assets != nil && h.assets.Hot()
	data["I18nHotReload"] = i18n.Hot()
	data["PluginManagerAvailable"] = h.pluginManager != nil
	if h.pluginManager != nil {
		data["EnabledPluginIDs"] = strings.Join(h.pluginManager.EnabledIDs(c), ", ")
	}
	data["ShowSQLDetails"] = settings[consts.SettingsShowSQLDetails] == "true"
	data["UpdateDownloadMirrorValue"] = strings.TrimSpace(settings[consts.SettingsUpdateDownloadMirror])
	logLevel := strings.TrimSpace(settings[consts.SettingsLogLevel])
	if logLevel == "" {
		logLevel = consts.SettingsLogLevelDefault
	}
	data["LogLevelValue"] = logLevel
	data["SettingsGeneralURL"] = settingsPageURL("general")
	data["SettingsDeveloperURL"] = settingsPageURL("developer")
	data["InstanceRawVersion"] = version.Version
	updateLogPath := h.updateLogPath()
	data["UpdateLogPath"] = updateLogPath
	data["AppUpdateRunning"] = h.appUpdateInProgress()
	if data["AppUpdateRunning"] == true {
		data["AutoRefreshSeconds"] = 3
	}
	if logTail := readUpdateLogTail(updateLogPath, 12<<10); logTail != "" {
		data["UpdateLogTail"] = logTail
	}
	data["UpdateAvailable"] = false
	if c != nil && c.Query("check_update") == "1" {
		latest, err := latestRelease(c.Request.Context(), h.log)
		if err != nil {
			data["UpdateCheckError"] = err.Error()
		} else {
			data["LatestRelease"] = latest
			data["UpdateAvailable"] = latest.TagName != "" && latest.TagName != version.Version
		}
	}
	// 主题信息
	if h.themeManager != nil {
		current := h.themeManager.Current(c)
		if current != nil {
			data["CurrentThemeName"] = current.Name
			data["CurrentThemeDir"] = current.Dir
			data["CurrentThemeVersion"] = current.Version
		}
	}
	if c != nil && c.Query("message") == "templates-reloaded" {
		data["Notice"] = tr.T("模板已重新解析。")
	}
	if c != nil && c.Query("message") == "templates-released" {
		data["Notice"] = tr.T("已切换到本地模板 hot 模式；若 `web/templates` 已存在，则会直接复用现有文件，不覆盖你的改动。")
	}
	if c != nil && c.Query("message") == "templates-embedded" {
		data["Notice"] = tr.T("已切回使用内嵌模板资源；本地模板目录会保留在磁盘上。")
	}
	if c != nil && c.Query("message") == "assets-released" {
		data["Notice"] = tr.T("已切换到本地资源 hot 模式；若 `web/assets` 已存在，则会直接复用现有文件，不覆盖你的改动。")
	}
	if c != nil && c.Query("message") == "assets-embedded" {
		data["Notice"] = tr.T("已切回使用内嵌资源文件；本地资源目录会保留在磁盘上。")
	}
	if c != nil && c.Query("message") == "i18n-released" {
		data["Notice"] = tr.T("已切换到本地翻译资源 hot 模式；若 `web/i18n` 已存在，则会直接复用现有文件，不覆盖你的改动。")
	}
	if c != nil && c.Query("message") == "i18n-embedded" {
		data["Notice"] = tr.T("已切回使用内嵌翻译资源；本地翻译目录会保留在磁盘上。")
	}
	if c != nil && c.Query("message") == "smtp-saved" {
		data["Notice"] = tr.T("SMTP 设置已保存。")
	}
	if c != nil && c.Query("message") == "smtp-test-sent" {
		data["Notice"] = tr.T("测试邮件已发送，请检查收件箱。")
	}
	if c != nil && c.Query("message") == "metrics-saved" {
		data["Notice"] = tr.T("Metrics Basic Auth 密码已保存。")
	}
	if c != nil && c.Query("message") == "sql-details-saved" {
		data["Notice"] = tr.T("SQL 调试设置已保存。")
	}
	if c != nil && c.Query("message") == "log-level-saved" {
		data["Notice"] = tr.T("日志级别已保存并立即生效。")
	}
	if c != nil && c.Query("message") == "update-settings-saved" {
		data["Notice"] = tr.T("更新设置已保存。")
	}
	if c != nil && c.Query("message") == "theme-reloaded" {
		data["Notice"] = tr.T("主题已重载。")
	}
	if c != nil && c.Query("message") == "plugins-reloaded" {
		data["Notice"] = tr.T("插件资源已重载。")
	}
	if c != nil && c.Query("message") == "app-update-started" {
		data["Notice"] = tr.T("后台更新已启动，请稍等片刻后刷新页面确认版本。")
	}
	if c != nil && c.Query("message") == "app-update-running" {
		data["Notice"] = tr.T("后台更新正在进行中，请稍后刷新页面确认版本。")
	}
	if c != nil && c.Query("message") == "registration-open-requires-smtp" {
		data["Error"] = tr.T("开放注册需要先配置 SMTP 邮件设置。")
	}
	if u := h.currentUser(c); u != nil {
		data["CurrentUser"] = u
	}
	return data
}

type latestReleaseInfo struct {
	TagName string
	Name    string
	HTMLURL string
}

func latestRelease(ctx context.Context, log *slog.Logger) (*latestReleaseInfo, error) {
	var errs []string
	for _, endpoint := range goProxyLatestURLs {
		attemptCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		release, err := latestReleaseFromGoProxy(attemptCtx, endpoint)
		cancel()
		if err == nil {
			return release, nil
		}
		errs = append(errs, err.Error())
		if log != nil {
			log.WarnContext(ctx, "check Go module proxy latest version failed", "url", endpoint, "error", err)
		}
	}
	attemptCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	release, err := latestReleaseFromGitHubRedirect(attemptCtx, log)
	cancel()
	if err == nil {
		return release, nil
	}
	errs = append(errs, err.Error())
	return nil, fmt.Errorf("检查最新版本失败：%s", strings.Join(errs, "; "))
}

type goProxyLatestInfo struct {
	Version string `json:"Version"`
}

func latestReleaseFromGoProxy(ctx context.Context, endpoint string) (*latestReleaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "wenlog-admin-updater")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Go Module Proxy 更新检查失败(%s): %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("Go Module Proxy 更新检查失败(%s): 返回 %d %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var latest goProxyLatestInfo
	if err := json.NewDecoder(resp.Body).Decode(&latest); err != nil {
		return nil, fmt.Errorf("解析 Go Module Proxy 响应失败(%s): %w", endpoint, err)
	}
	tag := strings.TrimSpace(latest.Version)
	if tag == "" {
		return nil, fmt.Errorf("Go Module Proxy 未返回版本号: %s", endpoint)
	}
	return &latestReleaseInfo{TagName: tag, Name: tag, HTMLURL: releaseHTMLURL(tag)}, nil
}

func latestReleaseFromGitHubRedirect(ctx context.Context, log *slog.Logger) (*latestReleaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, githubLatestReleaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "wenlog-admin-updater")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if log != nil {
			attrs := []any{
				"url", githubLatestReleaseURL,
				"resolved_url", resp.Request.URL.String(),
				"status", resp.Status,
				"status_code", resp.StatusCode,
				"x_github_request_id", resp.Header.Get("X-GitHub-Request-Id"),
				"body", strings.TrimSpace(string(body)),
			}
			if readErr != nil {
				attrs = append(attrs, "body_read_error", readErr)
			}
			log.ErrorContext(ctx, "check GitHub release redirect failed", attrs...)
		}
		return nil, fmt.Errorf("GitHub Release 更新检查失败：返回 %d；详情请查看服务端日志", resp.StatusCode)
	}
	tag, err := releaseTagFromURL(resp.Request.URL)
	if err != nil {
		return nil, err
	}
	return &latestReleaseInfo{TagName: tag, Name: tag, HTMLURL: releaseHTMLURL(tag)}, nil
}

func releaseTagFromURL(u *url.URL) (string, error) {
	if u == nil {
		return "", fmt.Errorf("GitHub 最新 Release 跳转地址为空")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "releases" && parts[i+1] == "tag" && i+2 < len(parts) {
			tag, err := url.PathUnescape(parts[i+2])
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(tag) != "" {
				return tag, nil
			}
		}
	}
	return "", fmt.Errorf("GitHub 最新 Release 跳转地址未包含版本标签: %s", u.String())
}

func releaseHTMLURL(tag string) string {
	return "https://github.com/youthlin/wenlog/releases/tag/" + url.PathEscape(tag)
}

// CheckAppUpdate 查询 GitHub 公开 Release 的最新版本并回到开发设置页展示结果。
func (h *Admin) CheckAppUpdate(c *gin.Context) {
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "")+"?check_update=1")
}

// ApplyAppUpdate 启动后台更新任务，避免下载过程被浏览器或反向代理请求超时中断。
func (h *Admin) ApplyAppUpdate(c *gin.Context) {
	logPath := h.updateLogPath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		h.serverError(c, err)
		return
	}
	if !h.beginAppUpdate() {
		c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "app-update-running"))
		return
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		h.finishAppUpdate()
		h.serverError(c, err)
		return
	}
	go h.runAppUpdate(context.Background(), logf, logPath)
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "app-update-started"))
}

func (h *Admin) beginAppUpdate() bool {
	if h == nil {
		return false
	}
	h.appUpdateMu.Lock()
	defer h.appUpdateMu.Unlock()
	if h.appUpdateRunning {
		return false
	}
	h.appUpdateRunning = true
	return true
}

func (h *Admin) finishAppUpdate() {
	if h == nil {
		return
	}
	h.appUpdateMu.Lock()
	h.appUpdateRunning = false
	h.appUpdateMu.Unlock()
}

func (h *Admin) appUpdateInProgress() bool {
	if h == nil {
		return false
	}
	h.appUpdateMu.Lock()
	defer h.appUpdateMu.Unlock()
	return h.appUpdateRunning
}

func (h *Admin) runAppUpdate(ctx context.Context, logf *os.File, logPath string) {
	defer func() {
		_ = logf.Close()
		h.finishAppUpdate()
	}()
	fprintfUpdateLog(logf, "后台更新任务已启动\n")
	tag, err := h.applyNativeUpdate(ctx, logf)
	if err != nil {
		fprintfUpdateLog(logf, "更新失败: %v\n", err)
		if h.log != nil {
			h.log.ErrorContext(ctx, "native app update failed", "error", err, "log_path", logPath)
		}
		return
	}
	fprintfUpdateLog(logf, "更新任务已完成，准备重启服务\n")
	if h.log != nil {
		h.log.InfoContext(ctx, "native app update installed", "version", tag)
	}
	h.scheduleRestartAfterUpdate(ctx)
}

func (h *Admin) updateLogPath() string {
	if h != nil && h.cfg != nil && strings.TrimSpace(h.cfg.DBPath) != "" {
		return filepath.Join(filepath.Dir(h.cfg.DBPath), "wenlog-update.log")
	}
	return filepath.Join("data", "wenlog-update.log")
}

func readUpdateLogTail(path string, maxBytes int64) string {
	path = strings.TrimSpace(path)
	if path == "" || maxBytes <= 0 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() == 0 {
		return ""
	}
	offset := info.Size() - maxBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	if offset > 0 {
		if idx := bytes.IndexByte(data, '\n'); idx >= 0 && idx+1 < len(data) {
			data = data[idx+1:]
		}
	}
	return strings.TrimSpace(string(data))
}

func (h *Admin) applyNativeUpdate(ctx context.Context, logw io.Writer) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	latest, err := latestRelease(ctx, h.log)
	if err != nil {
		return "", err
	}
	if latest.TagName == version.Version {
		return latest.TagName, nil
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return "", fmt.Errorf("当前平台暂不支持后台一键更新: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	asset := fmt.Sprintf("wenlog-%s-%s-%s.tar.gz", latest.TagName, runtime.GOOS, runtime.GOARCH)
	settings, err := h.st.GetSettings(ctx, consts.SettingsUpdateDownloadMirror)
	if err != nil && h.log != nil {
		h.log.ErrorContext(ctx, "get update download mirror setting", "error", err)
	}
	mirror := strings.TrimSpace(settings[consts.SettingsUpdateDownloadMirror])
	downloadSources := updateDownloadSources(latest.TagName, asset, mirror)
	exe, err := currentUpdatableExecutablePath()
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "wenlog-update-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	archivePath := filepath.Join(tmp, asset)
	checksumPath := archivePath + ".sha256"
	fprintfUpdateLog(logw, "准备更新到 %s (%s/%s)\n", latest.TagName, runtime.GOOS, runtime.GOARCH)
	if mirror != "" {
		fprintfUpdateLog(logw, "使用下载镜像：%s\n", mirror)
	}
	if err := downloadAndVerifyUpdateAsset(ctx, downloadSources, archivePath, checksumPath, logw); err != nil {
		return "", err
	}
	extracted, err := extractWenlogBinary(archivePath, tmp)
	if err != nil {
		return "", err
	}
	backup := fmt.Sprintf("%s.bak.%s", exe, time.Now().Format("20060102150405"))
	if err := copyFile(exe, backup, 0o755); err != nil {
		return "", fmt.Errorf("备份当前二进制失败: %w", err)
	}
	if err := installUpdatedBinary(extracted, exe); err != nil {
		_ = installUpdatedBinary(backup, exe)
		return "", fmt.Errorf("替换二进制失败，已尝试回滚: %w", err)
	}
	fprintfUpdateLog(logw, "更新完成: %s -> %s，备份: %s\n", exe, latest.TagName, backup)
	return latest.TagName, nil
}

func fprintfUpdateLog(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "%s ", time.Now().Format(time.RFC3339))
	_, _ = fmt.Fprintf(w, format, args...)
}

func downloadReleaseFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "wenlog-admin-updater")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 %s 失败，状态码 %d", url, resp.StatusCode)
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

type updateDownloadSource struct {
	Name        string
	ArchiveURL  string
	ChecksumURL string
}

func updateDownloadSources(tag, asset, mirror string) []updateDownloadSource {
	githubArchiveURL := releaseDownloadURL(githubReleaseBaseURL, tag, asset)
	if strings.TrimSpace(mirror) != "" {
		return []updateDownloadSource{{
			Name:        "下载镜像",
			ArchiveURL:  mirroredDownloadURL(githubArchiveURL, mirror),
			ChecksumURL: mirroredDownloadURL(githubArchiveURL+".sha256", mirror),
		}}
	}
	giteeArchiveURL := releaseDownloadURL(giteeReleaseBaseURL, tag, asset)
	return []updateDownloadSource{
		{Name: "Gitee", ArchiveURL: giteeArchiveURL, ChecksumURL: giteeArchiveURL + ".sha256"},
		{Name: "GitHub", ArchiveURL: githubArchiveURL, ChecksumURL: githubArchiveURL + ".sha256"},
	}
}

func releaseDownloadURL(baseURL, tag, asset string) string {
	return strings.TrimRight(baseURL, "/") + "/" + url.PathEscape(tag) + "/" + url.PathEscape(asset)
}

func downloadAndVerifyUpdateAsset(ctx context.Context, sources []updateDownloadSource, archivePath, checksumPath string, logw io.Writer) error {
	var errs []string
	for _, source := range sources {
		fprintfUpdateLog(logw, "尝试从 %s 下载更新包\n", source.Name)
		if err := downloadReleaseFile(ctx, source.ArchiveURL, archivePath); err != nil {
			errs = append(errs, fmt.Sprintf("%s 下载压缩包失败: %v", source.Name, err))
			fprintfUpdateLog(logw, "%s 下载压缩包失败: %v\n", source.Name, err)
			continue
		}
		if err := downloadReleaseFile(ctx, source.ChecksumURL, checksumPath); err != nil {
			errs = append(errs, fmt.Sprintf("%s 下载校验文件失败: %v", source.Name, err))
			fprintfUpdateLog(logw, "%s 下载校验文件失败: %v\n", source.Name, err)
			continue
		}
		if err := verifySHA256File(archivePath, checksumPath); err != nil {
			errs = append(errs, fmt.Sprintf("%s 校验更新包失败: %v", source.Name, err))
			fprintfUpdateLog(logw, "%s 校验更新包失败: %v\n", source.Name, err)
			continue
		}
		fprintfUpdateLog(logw, "已从 %s 下载更新包\n", source.Name)
		return nil
	}
	return fmt.Errorf("下载更新包失败：%s", strings.Join(errs, "; "))
}

func mirroredDownloadURL(rawURL, mirror string) string {
	mirror = strings.TrimSpace(mirror)
	if mirror == "" {
		return rawURL
	}
	if strings.Contains(mirror, "{raw_url}") {
		return strings.ReplaceAll(mirror, "{raw_url}", rawURL)
	}
	if strings.Contains(mirror, "{url}") {
		return strings.ReplaceAll(mirror, "{url}", url.QueryEscape(rawURL))
	}
	if strings.Contains(mirror, "%s") {
		return fmt.Sprintf(mirror, rawURL)
	}
	return strings.TrimRight(mirror, "/") + "/" + rawURL
}

// SaveUpdateSettings 保存后台更新相关设置。
func (h *Admin) SaveUpdateSettings(c *gin.Context) {
	tr := i18n.Get(c)
	mirror := strings.TrimSpace(c.PostForm("update_download_mirror"))
	if mirror != "" {
		probeURL := mirroredDownloadURL("https://github.com/youthlin/wenlog/releases/download/v0.0.0/wenlog-v0.0.0-linux-amd64.tar.gz", mirror)
		parsed, err := url.Parse(probeURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			data := h.settingsDataForTab(c, "developer")
			data["Error"] = tr.T("下载镜像地址不合法，请填写 https 地址。")
			data["UpdateDownloadMirrorValue"] = mirror
			c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
			return
		}
	}
	if err := h.st.SetSetting(c, consts.SettingsUpdateDownloadMirror, mirror); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "update-settings-saved"))
}

func verifySHA256File(filePath, checksumPath string) error {
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		return err
	}
	fields := strings.Fields(string(checksumData))
	if len(fields) == 0 {
		return fmt.Errorf("checksum 文件为空")
	}
	want := strings.ToLower(strings.TrimSpace(fields[0]))
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := fmt.Sprintf("%x", h.Sum(nil))
	if got != want {
		return fmt.Errorf("checksum 不匹配: got %s, want %s", got, want)
	}
	return nil
}

func extractWenlogBinary(archivePath, targetDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.FileInfo().IsDir() || filepath.Base(hdr.Name) != "wenlog" {
			continue
		}
		outPath := filepath.Join(targetDir, "wenlog")
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return outPath, nil
	}
	return "", fmt.Errorf("压缩包中未找到 wenlog 二进制")
}

func currentExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		return resolved, nil
	}
	return exe, nil
}

func currentUpdatableExecutablePath() (string, error) {
	exe, err := currentExecutablePath()
	if err != nil {
		return "", err
	}
	if looksLikeGoRunTempExecutable(exe) {
		return "", fmt.Errorf("当前进程由 go run 临时二进制启动，无法原地替换；请先执行 go build -o wenlog ./cmd/server，再用 ./wenlog restart 启动后重试")
	}
	info, err := os.Stat(exe)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("当前二进制不存在，无法原地更新: %s；如果是 go run ./cmd/server restart 启动，请先执行 go build -o wenlog ./cmd/server，再用 ./wenlog restart 启动后重试", exe)
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("当前可执行路径是目录，无法原地更新: %s", exe)
	}
	return exe, nil
}

func looksLikeGoRunTempExecutable(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.Contains(clean, "/go-build") && strings.Contains(clean, "/exe/")
}

func installUpdatedBinary(src, dest string) error {
	tmpDest := fmt.Sprintf("%s.new.%d", dest, time.Now().UnixNano())
	if err := copyFile(src, tmpDest, 0o755); err != nil {
		return err
	}
	return os.Rename(tmpDest, dest)
}

func copyFile(src, dest string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(dest, perm)
}

func (h *Admin) scheduleRestartAfterUpdate(ctx context.Context) {
	go func() {
		time.Sleep(1500 * time.Millisecond)
		if runningUnderSystemd() {
			os.Exit(3)
		}
		exe, err := currentExecutablePath()
		if err == nil {
			cmd := exec.Command(exe, "restart")
			cmd.Dir = mustGetwdForUpdate()
			cmd.Env = os.Environ()
			if startErr := cmd.Start(); startErr == nil {
				_ = cmd.Process.Release()
				return
			} else if h != nil && h.log != nil {
				h.log.ErrorContext(ctx, "start self restart after update", "error", startErr)
			}
		}
		os.Exit(3)
	}()
}

func runningUnderSystemd() bool {
	return strings.TrimSpace(os.Getenv("INVOCATION_ID")) != "" || strings.TrimSpace(os.Getenv("JOURNAL_STREAM")) != ""
}

func mustGetwdForUpdate() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func (h *Admin) ensureMetricsAuthPassword(ctx context.Context, current string) string {
	password := strings.TrimSpace(current)
	if password != "" {
		return password
	}
	password = util.GenerateRandomString(consts.TokenLengthMetrics, util.WithAlphaNumer())
	if h != nil && h.st != nil {
		_ = h.st.SetSetting(ctx, consts.SettingsMetricsAuthPassword, password)
	}
	return password
}

func (h *Admin) settingsDataForTab(c *gin.Context, tab string) gin.H {
	return h.settingsDataForSection(c, tab)
}

// SaveSiteSettings 保存站点名称/描述。
func (h *Admin) SaveSiteSettings(c *gin.Context) {
	tr := i18n.Get(c)
	name := strings.TrimSpace(c.PostForm("site_name"))
	desc := strings.TrimSpace(c.PostForm("site_description"))
	siteLogo := strings.TrimSpace(c.PostForm("site_logo"))
	postPermalink := strings.TrimSpace(c.PostForm("post_permalink"))
	categoryPrefix := strings.TrimSpace(c.PostForm("category_prefix"))
	tagPrefix := strings.TrimSpace(c.PostForm("tag_prefix"))
	pageSize, err := strconv.Atoi(strings.TrimSpace(c.PostForm("page_size")))
	if err != nil || pageSize < 1 {
		pageSize = defaultPublicPageSize
	}
	feedSize := positiveIntSetting(c.PostForm("feed_size"), defaultFeedSize)
	defaultAvatar := util.NormalizeDefaultAvatar(c.PostForm("default_avatar"))

	catNorm, tagNorm, ok := h.validateSiteSettingsPermalink(c, tr, postPermalink, categoryPrefix, tagPrefix)
	if !ok {
		return
	}

	settings := map[string]string{
		consts.SettingsSiteName:       name,
		consts.SettingsSiteDesc:       desc,
		consts.SettingsSiteLogo:       siteLogo,
		consts.SettingsPostPermalink:  permalink.NormalizePostPattern(postPermalink),
		consts.SettingsCategoryPrefix: catNorm,
		consts.SettingsTagPrefix:      tagNorm,
		consts.SettingsPageSize:       strconv.Itoa(pageSize),
		consts.SettingsFeedSize:       strconv.Itoa(feedSize),
		consts.SettingsDefaultAvatar:  defaultAvatar,
	}
	if err := h.setSettings(c, settings); err != nil {
		h.serverError(c, err)
		return
	}

	registrationOpen := c.PostForm("registration_open") == "on"
	if registrationOpen && !smtpConfigFromStore(c, h.st).Configured() {
		_ = h.st.SetSetting(c, consts.SettingsRegistrationOpen, "false")
		c.Redirect(http.StatusSeeOther, settingsRedirectURL("general", "registration-open-requires-smtp"))
		return
	}
	_ = h.st.SetSetting(c, consts.SettingsRegistrationOpen, strconv.FormatBool(registrationOpen))
	syncPostPermalink(c, h.st)
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("general", ""))
}

// setSettings 批量保存设置,遇到第一个错误即返回。
func (h *Admin) setSettings(ctx context.Context, kv map[string]string) error {
	for k, v := range kv {
		if err := h.st.SetSetting(ctx, k, v); err != nil {
			return err
		}
	}
	return nil
}

func (h *Admin) validateSiteSettingsPermalink(c *gin.Context, tr *gettext.Translations, postPermalink, categoryPrefix, tagPrefix string) (catNorm, tagNorm string, ok bool) {
	if err := permalink.ValidatePostPattern(postPermalink); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("固定链接结构不合法: %s", err.Error())
		data["PostPermalinkValue"] = permalink.NormalizePostPattern(postPermalink)
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return "", "", false
	}
	if err := permalink.ValidateTaxonomyPrefix(categoryPrefix); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("分类目录前缀不合法: %s", err.Error())
		data["CategoryPrefixValue"] = permalink.NormalizeTaxonomyPrefix(categoryPrefix, consts.SettingsCategoryPrefixDefault)
		data["TagPrefixValue"] = permalink.NormalizeTaxonomyPrefix(tagPrefix, consts.SettingsTagPrefixDefault)
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return "", "", false
	}
	if err := permalink.ValidateTaxonomyPrefix(tagPrefix); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("标签前缀不合法: %s", err.Error())
		data["CategoryPrefixValue"] = permalink.NormalizeTaxonomyPrefix(categoryPrefix, consts.SettingsCategoryPrefixDefault)
		data["TagPrefixValue"] = permalink.NormalizeTaxonomyPrefix(tagPrefix, consts.SettingsTagPrefixDefault)
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return "", "", false
	}
	catNorm = permalink.NormalizeTaxonomyPrefix(categoryPrefix, consts.SettingsCategoryPrefixDefault)
	tagNorm = permalink.NormalizeTaxonomyPrefix(tagPrefix, consts.SettingsTagPrefixDefault)
	if catNorm == tagNorm {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("分类目录前缀和标签前缀不能相同。")
		data["CategoryPrefixValue"] = catNorm
		data["TagPrefixValue"] = tagNorm
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return "", "", false
	}
	return catNorm, tagNorm, true
}

// SaveSMTPSettings 只保存 SMTP 邮件设置,避免触发站点设置必填项校验。
func (h *Admin) SaveSMTPSettings(c *gin.Context) {
	if err := h.saveSMTPSettings(c); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("general", "smtp-saved"))
}

// SaveMetricsAuthSettings 保存 /metrics Basic Auth 密码。
func (h *Admin) SaveMetricsAuthSettings(c *gin.Context) {
	tr := i18n.Get(c)
	password := strings.TrimSpace(c.PostForm("metrics_auth_password"))
	if len(password) < consts.MetricsPasswordMinLen {
		data := h.settingsDataForTab(c, "developer")
		data["Error"] = tr.T("Metrics Basic Auth 密码至少需要 12 个字符。")
		data["MetricsAuthPasswordValue"] = password
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if err := h.st.SetSetting(c, consts.SettingsMetricsAuthPassword, password); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "metrics-saved"))
}

// SaveSQLDetailsSettings 保存 SQL 详情输出开关。
func (h *Admin) SaveSQLDetailsSettings(c *gin.Context) {
	showSQL := c.PostForm("show_sql_details") == "on"
	if err := h.st.SetSetting(c, consts.SettingsShowSQLDetails, strconv.FormatBool(showSQL)); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "sql-details-saved"))
}

// SaveLogLevelSettings 保存日志级别设置并立即生效。
func (h *Admin) SaveLogLevelSettings(c *gin.Context) {
	level := strings.TrimSpace(strings.ToLower(c.PostForm("log_level")))
	if !middleware.SetLogLevel(level) {
		level = consts.SettingsLogLevelDefault
		middleware.SetLogLevel(level)
	}
	if err := h.st.SetSetting(c, consts.SettingsLogLevel, level); err != nil {
		h.serverError(c, err)
		return
	}
	slog.Info("log level changed", "level", level)
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "log-level-saved"))
}

func (h *Admin) saveSMTPSettings(c *gin.Context) error {
	smtpHost := strings.TrimSpace(c.PostForm("smtp_host"))
	smtpPort := strings.TrimSpace(c.PostForm("smtp_port"))
	smtpUser := strings.TrimSpace(c.PostForm("smtp_user"))
	smtpPassword := c.PostForm("smtp_password")
	if strings.TrimSpace(smtpPassword) == "" {
		settings, err := h.st.GetSettings(c, consts.SettingsSMTPPassword)
		if err != nil && h.log != nil {
			h.log.ErrorContext(c, "get smtp password setting", "error", err)
		}
		smtpPassword = settings[consts.SettingsSMTPPassword]
	}
	smtpFrom := strings.TrimSpace(c.PostForm("smtp_from"))
	siteURL := strings.TrimSpace(c.PostForm("site_url"))
	settings := map[string]string{
		consts.SettingsSMTPHost:     smtpHost,
		consts.SettingsSMTPPort:     smtpPort,
		consts.SettingsSMTPUser:     smtpUser,
		consts.SettingsSMTPPassword: smtpPassword,
		consts.SettingsSMTPFrom:     smtpFrom,
		consts.SettingsSiteURL:      siteURL,
	}
	if err := h.setSettings(c, settings); err != nil {
		return err
	}
	if !smtpConfigFromSettings(settings).Configured() {
		if err := h.st.SetSetting(c, consts.SettingsRegistrationOpen, "false"); err != nil {
			return err
		}
	}
	return nil
}

// TestSMTPSettings 保存当前表单中的 SMTP 配置后发送测试邮件。
func (h *Admin) TestSMTPSettings(c *gin.Context) {
	tr := i18n.Get(c)
	if err := h.saveSMTPSettings(c); err != nil {
		h.serverError(c, err)
		return
	}
	to := strings.TrimSpace(c.PostForm("test_email"))
	if to == "" {
		if u := h.currentUser(c); u != nil {
			to = strings.TrimSpace(u.Email)
		}
	}
	if to == "" {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("测试收件人不能为空。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if _, err := mail.ParseAddress(to); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("测试收件人邮箱格式不正确。")
		data["SMTPTestEmailValue"] = to
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	smtpCfg := smtpConfigFromStore(c, h.st)
	if !smtpCfg.Configured() {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("SMTP 未配置完整，请填写服务器、端口和发件人地址。")
		data["SMTPTestEmailValue"] = to
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	siteName := siteNameFromStore(c, h.st)
	siteURL := siteURLFromRequest(h.st, c)
	subject := tr.T("[%s] SMTP 测试邮件", siteName)
	body := tr.T("这是一封来自 %s 的 SMTP 测试邮件。\n\n如果你收到这封邮件，说明 SMTP 配置已生效。\n", siteName)
	body = mailBodyWithSiteDomain(tr, body, siteURL)
	if err := smtpCfg.Send(to, subject, body); err != nil {
		data := h.settingsDataForTab(c, "general")
		data["Error"] = tr.T("测试邮件发送失败: %s", err.Error())
		data["SMTPTestEmailValue"] = to
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("general", "smtp-test-sent"))
}

// SaveSessionSettings 重新生成 session secret。修改后所有登录用户都需要重新登录。
func (h *Admin) SaveSessionSettings(c *gin.Context) {
	secret := util.GenerateRandomString(32)
	if err := h.st.SetSetting(c, consts.SettingsSessionSecret, secret); err != nil {
		h.serverError(c, err)
		return
	}
	s := sessions.Default(c)
	s.Clear()
	_ = s.Save()
	c.Redirect(http.StatusSeeOther, "/auth/login?message=session-secret-updated")
}

// ReloadTemplates 手动重新解析本地模板文件。仅热更新模式支持。
func (h *Admin) ReloadTemplates(c *gin.Context) {
	tr := i18n.Get(c)
	if h.renderer == nil || !h.renderer.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前不是热更新模式，不能重新解析模板。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if err := h.renderer.Reload(); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("重新解析模板失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "templates-reloaded"))
}

// ReleaseTemplates 把嵌入模板释放到本地目录,并切换为 hot 模式。
func (h *Admin) ReleaseTemplates(c *gin.Context) {
	tr := i18n.Get(c)
	if h.renderer == nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前模板渲染器不可用。")
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if h.renderer.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前已经是 hot 模式，无需再次释放模板。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if util.PathExists("web/templates") {
		if err := h.renderer.UseFS(os.DirFS("web/templates"), true); err != nil {
			data := h.settingsDataForTab(c, "resources")
			data["Error"] = tr.T("切换到本地模板失败: %s", err.Error())
			c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
			return
		}
		c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "templates-released"))
		return
	}
	if err := h.renderer.ReleaseToHotDir("web/templates"); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("释放模板文件失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "templates-released"))
}

// UseEmbeddedTemplates 切回当前进程使用内嵌模板资源,但保留本地目录。
func (h *Admin) UseEmbeddedTemplates(c *gin.Context) {
	tr := i18n.Get(c)
	if h.renderer == nil || !h.renderer.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前不是 hot 模式，不能切回内嵌模板。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if err := h.renderer.ResetToDefault(); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("切换回内嵌模板失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "templates-embedded"))
}

// ReleaseAssets 把嵌入资源释放到本地目录。释放完成后当前进程会自动优先读取该目录。
func (h *Admin) ReleaseAssets(c *gin.Context) {
	tr := i18n.Get(c)
	if h.assets == nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前资源管理器不可用。")
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if h.assets.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前已经是 hot 模式，无需再次释放资源文件。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if util.PathExists("web/assets") {
		h.assets.SetHot(true)
		c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "assets-released"))
		return
	}
	assetsFS, err := fs.Sub(web.Assets, "assets")
	if err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("读取嵌入资源失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if err := releaseDirFromFS(assetsFS, "web/assets"); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("释放资源文件失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	h.assets.SetHot(true)
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "assets-released"))
}

// UseEmbeddedAssets 切回当前进程使用内嵌资源,但保留本地目录。
func (h *Admin) UseEmbeddedAssets(c *gin.Context) {
	tr := i18n.Get(c)
	if h.assets == nil || !h.assets.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前不是 hot 模式，不能切回内嵌资源。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	h.assets.SetHot(false)
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "assets-embedded"))
}

// ReleaseI18n 把嵌入翻译资源释放到本地目录,并切换当前进程优先读取本地目录。
func (h *Admin) ReleaseI18n(c *gin.Context) {
	tr := i18n.Get(c)
	if i18n.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前已经是 hot 模式，无需再次释放翻译资源。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	if !util.PathExists("web/i18n") {
		langFS, err := fs.Sub(web.I18n, "i18n")
		if err != nil {
			data := h.settingsDataForTab(c, "resources")
			data["Error"] = tr.T("读取内嵌翻译资源失败: %s", err.Error())
			c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
			return
		}
		if err := releaseDirFromFS(langFS, "web/i18n"); err != nil {
			data := h.settingsDataForTab(c, "resources")
			data["Error"] = tr.T("释放翻译资源失败: %s", err.Error())
			c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
			return
		}
	}
	i18n.SetHot(true)
	if err := i18n.Reload(); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("重新加载翻译资源失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if h.themeManager != nil {
		if err := h.themeManager.LoadTranslations(); err != nil {
			data := h.settingsDataForTab(c, "resources")
			data["Error"] = tr.T("重新加载主题翻译资源失败: %s", err.Error())
			c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
			return
		}
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "i18n-released"))
}

// UseEmbeddedI18n 切回当前进程使用内嵌翻译资源,但保留本地目录。
func (h *Admin) UseEmbeddedI18n(c *gin.Context) {
	tr := i18n.Get(c)
	if !i18n.Hot() {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("当前不是 hot 模式，不能切回内嵌翻译资源。")
		c.HTML(http.StatusBadRequest, "admin_settings.gohtml", data)
		return
	}
	i18n.SetHot(false)
	if err := i18n.Reload(); err != nil {
		data := h.settingsDataForTab(c, "resources")
		data["Error"] = tr.T("重新加载翻译资源失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if h.themeManager != nil {
		if err := h.themeManager.LoadTranslations(); err != nil {
			data := h.settingsDataForTab(c, "resources")
			data["Error"] = tr.T("重新加载主题翻译资源失败: %s", err.Error())
			c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
			return
		}
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("resources", "i18n-embedded"))
}

func releaseDirFromFS(src fs.FS, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// ReloadTheme 从开发设置页重载当前主题。
func (h *Admin) ReloadTheme(c *gin.Context) {
	tr := i18n.Get(c)
	if h.themeManager == nil {
		data := h.settingsDataForTab(c, "developer")
		data["Error"] = tr.T("主题管理器未初始化。")
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if err := h.themeManager.ReloadCurrentTheme(c); err != nil {
		data := h.settingsDataForTab(c, "developer")
		data["Error"] = tr.T("重载主题失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "theme-reloaded"))
}

// ReloadPlugins 从开发设置页重载所有已启用插件资源。
func (h *Admin) ReloadPlugins(c *gin.Context) {
	tr := i18n.Get(c)
	if h.pluginManager == nil {
		data := h.settingsDataForTab(c, "developer")
		data["Error"] = tr.T("插件管理器未初始化。")
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if err := h.reloadPluginRuntime(c); err != nil {
		data := h.settingsDataForTab(c, "developer")
		data["Error"] = tr.T("重载插件资源失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	if err := h.pluginManager.RebuildTranslations(); err != nil {
		data := h.settingsDataForTab(c, "developer")
		data["Error"] = tr.T("重载插件翻译资源失败: %s", err.Error())
		c.HTML(http.StatusInternalServerError, "admin_settings.gohtml", data)
		return
	}
	c.Redirect(http.StatusSeeOther, settingsRedirectURL("developer", "plugins-reloaded"))
}
