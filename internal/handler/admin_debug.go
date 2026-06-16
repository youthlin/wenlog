package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/i18n"
)

// DebugPage 是只读 SQL 调试页,以 JSON 展示结果集。
func (h *Admin) DebugPage(c *gin.Context) {
	tr := i18n.Get(c)
	if c.Request.Method != http.MethodPost {
		// pageData 中含有 i18n.Inject 注入的函数, 不能序列化为 JSON
		pageData := h.base(c, tr.T("DB Debug"))
		pageData["SQL"] = ""
		c.HTML(http.StatusOK, "admin_debug.gohtml", pageData)
		return
	}

	sqlText := strings.TrimSpace(c.PostForm("sql"))
	jsonData := gin.H{"SQL": sqlText}
	if !allowDebugSQL(sqlText) {
		jsonData["Error"] = tr.T("仅允许只读 SQL(SELECT/EXPLAIN)。")
		c.JSON(http.StatusBadRequest, jsonData)
		return
	}
	rows, err := h.st.DebugQuery(c, sqlText)
	if err != nil {
		h.log.ErrorContext(c, "SQL执行失败",
			slog.String("sql", sqlText),
			slog.Any("error", err),
		)
		jsonData["Error"] = err.Error()
		c.JSON(http.StatusBadRequest, jsonData)
		return
	}
	h.log.InfoContext(c, "SQL执行成功",
		slog.String("sql", sqlText),
	)
	jsonData["Result"] = rows
	c.JSON(http.StatusOK, jsonData)
}

func allowDebugSQL(sqlText string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sqlText))
	// 拒绝包含分号的多语句,防止 SELECT 1; DROP TABLE users;-- 这类注入。
	if strings.Contains(upper, ";") {
		return false
	}
	return strings.HasPrefix(upper, "SELECT") ||
		strings.HasPrefix(upper, "EXPLAIN")
}
