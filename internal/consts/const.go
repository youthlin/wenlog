package consts

const (
	// 站点标题
	SettingsSiteName        = "site_name"
	SettingsSiteNameDefault = "我的博客"
	// 站点描述
	SettingsSiteDesc = "site_description"
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
	// 博主动态用哪个postid
	SettingsSayingPageID        = "saying_page_id"
	SettingsSayingPageIDDefault = 456
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
)
