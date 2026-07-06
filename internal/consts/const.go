package consts

import "time"

const (
	// 站点标题
	SettingsSiteName        = "site_name"
	SettingsSiteNameDefault = "我的博客"
	// 站点描述
	SettingsSiteDesc = "site_description"
	// 站点 Logo URL
	SettingsSiteLogo = "site_logo"
	// 首页文章分页数量
	SettingsPageSize = "page_size"
	// Feed 输出数量
	SettingsFeedSize = "feed_size"
	// 固定链接
	SettingsPostPermalink        = "post_permalink"
	SettingsPostPermalinkDefault = "/%year%%post_id%.html"
	// 分类目录前缀
	SettingsCategoryPrefix        = "category_prefix"
	SettingsCategoryPrefixDefault = "category"
	// 标签前缀
	SettingsTagPrefix        = "tag_prefix"
	SettingsTagPrefixDefault = "tag"
	// Cravatar 默认头像类型
	SettingsDefaultAvatar        = "default_avatar"
	SettingsDefaultAvatarDefault = "mp"
	// 会话密钥
	SettingsSessionSecret = "session_secret"
	// Metrics Basic Auth 密码,用户名固定为 metrics。
	SettingsMetricsAuthPassword = "metrics_auth_password"
	// 是否开放注册
	SettingsRegistrationOpen = "registration_open"
	// SMTP 邮件配置
	SettingsSMTPHost     = "smtp_host"
	SettingsSMTPPort     = "smtp_port"
	SettingsSMTPUser     = "smtp_user"
	SettingsSMTPPassword = "smtp_password"
	SettingsSMTPFrom     = "smtp_from"
	// 站点 URL(用于生成重置密码链接)
	SettingsSiteURL = "site_url"
	// 是否在 HTML 响应中输出 SQL 执行详情(仅管理员登录时)
	SettingsShowSQLDetails = "show_sql_details"
	// GitHub Release 资产下载镜像;为空时直接从 GitHub 下载。
	SettingsUpdateDownloadMirror = "update_download_mirror"
	// 自动备份设置
	SettingsAutoBackupEnabled     = "auto_backup_enabled"
	SettingsAutoBackupTime        = "auto_backup_time"
	SettingsAutoBackupKeep        = "auto_backup_keep"
	SettingsAutoBackupTimeDefault = "03:00"
	SettingsAutoBackupKeepDefault = 10
)

// 安全相关常量。
const (
	TokenLengthVerification = 32 // 邮箱验证/密码重置 token 长度(字节)
	TokenLengthUpload       = 24 // 上传文件名随机串长度
	TokenLengthMetrics      = 24 // Metrics 密码随机串长度
	PasswordMinLen          = 8  // 密码最小长度
	MetricsPasswordMinLen   = 12 // Metrics Basic Auth 密码最小长度
	TimingAttackDelay       = 50 // 防时序攻击延迟(毫秒)
)

// 超时相关常量。
const (
	VerificationTokenTTL = 24 * time.Hour // 邮箱验证/密码重置 token 有效期
	ResetTokenTTL        = 1 * time.Hour  // 密码重置 token 有效期
)

// 上传相关常量。
const (
	MaxUploadSize = 10 << 20 // 上传文件最大 10MB
)

// 展示相关常量。
const (
	AvatarSizeSmall       = 48 // 头像尺寸(像素)
	CommentSnippetMaxRune = 36 // 评论摘要最大字符数
)

// 会话相关常量。
const (
	SessionMaxAge = 7 * 86400 // 会话有效期(秒), 7 天
)

// 默认分类 slug。
const (
	DefaultCategorySlug = "uncategorized"
)
