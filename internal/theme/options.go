package theme

import "fmt"

// OptionKey 返回 option 在 Setting 表中的 key。
// 格式：option_<theme_name>_<option_id>
func OptionKey(themeName, optionID string) string {
	return fmt.Sprintf("option_%s_%s", themeName, optionID)
}

// GetOption 从 Setting 表读取 option 值，未配置时返回 default 值。
// getSetting 是 store.GetSetting 的函数签名。
func GetOption(getSetting func(key string) (string, error), themeName string, opt OptionDecl) string {
	key := OptionKey(themeName, opt.ID)
	val, err := getSetting(key)
	if err != nil || val == "" {
		return opt.Default
	}
	return val
}
