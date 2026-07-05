// Package version 保存构建时注入的版本信息。
package version

// 以下变量由 GitHub Actions 通过 -ldflags 注入。
// 本地直接 go build 时保留 dev/unknown，方便区分未发布构建。
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Display 返回适合页面展示的实例版本。
func Display() string {
	if Commit == "" || Commit == "unknown" {
		return Version
	}
	short := Commit
	if len(short) > 7 {
		short = short[:7]
	}
	return Version + " (" + short + ")"
}
