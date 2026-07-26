package hook

import (
	"fmt"
	"strings"
)

// PluginOption 读取插件全局选项，未配置时返回 def。
func (api *API) PluginOption(optionID, def string) string {
	loader := api.loader()
	if loader == nil || optionID == "" {
		return def
	}
	pluginID := strings.TrimPrefix(api.domain, "plugin_")
	if pluginID == "" || pluginID == api.domain {
		return def
	}
	value := loader.GetSetting("plugin_" + pluginID + "_" + optionID)
	if value == "" {
		return def
	}
	return value
}

// Setting 读取站点设置项。
func (api *API) Setting(key string) string {
	loader := api.loader()
	if loader == nil || key == "" {
		return ""
	}
	return loader.GetSetting(key)
}

// Settings 批量读取站点设置项。
func (api *API) Settings(keys ...string) map[string]string {
	loader := api.loader()
	if loader == nil {
		return nil
	}
	return loader.GetSettings(keys...)
}

// WithThemeOptions 设置主题声明的选项列表（含默认值），供 GetOption 回退使用。
func (api *API) WithThemeOptions(opts []OptionDecl) *API {
	api.options = opts
	return api
}

// GetOption 读取主题选项，未配置时回退到 theme.yaml 中的默认值。
func (api *API) GetOption(themeName, optionID string) string {
	loader := api.loader()
	if loader == nil || themeName == "" || optionID == "" {
		return ""
	}
	return GetOptionByID(func(key string) (string, error) {
		return loader.GetSetting(key), nil
	}, themeName, api.options, optionID)
}

// OptionKey 返回 option 在 Setting 表中的 key。
func OptionKey(themeName, optionID string) string {
	return fmt.Sprintf("option_%s_%s", themeName, optionID)
}

// GetOption 从 Setting 表读取 option 值，未配置时返回 default 值。
func GetOption(getSetting func(key string) (string, error), themeName string, opt OptionDecl) string {
	key := OptionKey(themeName, opt.ID)
	val, err := getSetting(key)
	if err != nil || val == "" {
		return opt.Default
	}
	return val
}

// GetOptionByID 从选项声明列表中查找指定 option 并读取其配置值。
func GetOptionByID(getSetting func(key string) (string, error), themeName string, options []OptionDecl, optionID string) string {
	for _, opt := range options {
		if opt.ID == optionID {
			return GetOption(getSetting, themeName, opt)
		}
	}
	return ""
}
