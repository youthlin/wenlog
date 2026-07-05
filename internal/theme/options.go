package theme

import (
	"fmt"
)

// OptionKey 返回 option 在 Setting 表中的 key。
// 格式：option_<theme_name>_<option_id>
func OptionKey(themeName, optionID string) string {
	return fmt.Sprintf("option_%s_%s", themeName, optionID)
}
