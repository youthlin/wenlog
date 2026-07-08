package hook

import (
	"fmt"
	"html"
)

// Print 向当前 action 的输出 writer 写入字符串。
// 仅在 action handler 内部调用有效；filter 中无 writer 注入。
func (api *API) Print(args ...any) {
	if api == nil || api.ctx == nil {
		return
	}
	w := GetActionWriter(api.ctx)
	if w == nil {
		return
	}
	_, _ = fmt.Fprint(w, args...)
}

// Printf 向当前 action 的输出 writer 写入格式化字符串。
// 仅在 action handler 内部调用有效；filter 中无 writer 注入。
func (api *API) Printf(format string, args ...any) {
	if api == nil || api.ctx == nil {
		return
	}
	w := GetActionWriter(api.ctx)
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, format, args...)
}

// Println 向当前 action 的输出 writer 写入格式化字符串并自动换行。
func (api *API) Println(format string, args ...any) {
	api.Printf(format+"\n", args...)
}

// EscapeHTML 转义 HTML 特殊字符。
func (api *API) EscapeHTML(s string) string { return html.EscapeString(s) }
