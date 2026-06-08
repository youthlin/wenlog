// Package permalink 集中管理永久链接的生成与解析规则,前台链接生成与
// 旧链接重定向共用同一份规则,确保历史链接不变。
//
// 规则:
//   - 文章:/{发布年份}{post_id}.html       例:post_id=8 发布于 2012 → /20128.html
//   - 页面:/{slug}                          例:/about
package permalink

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/youthlin/blog/internal/model"
)

// postPathRe 匹配文章永久链接 /{4位年份}{数字id}.html。
var postPathRe = regexp.MustCompile(`^/(\d{4})(\d+)\.html$`)

// Post 返回文章的永久链接路径,如 /20128.html。
func Post(p *model.Post) string {
	return fmt.Sprintf("/%d%d.html", p.PublishedAt.Year(), p.ID)
}

// Page 返回页面的永久链接路径,如 /about。
func Page(p *model.Post) string {
	return "/" + p.Slug
}

// ParsePostPath 解析文章永久链接,返回 year 和 id。
// ok 为 false 表示路径不是文章永久链接格式。
func ParsePostPath(path string) (year int, id uint, ok bool) {
	m := postPathRe.FindStringSubmatch(path)
	if m == nil {
		return 0, 0, false
	}
	y, _ := strconv.Atoi(m[1])
	n, err := strconv.ParseUint(m[2], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return y, uint(n), true
}

// Category 返回分类页路径。
func Category(slug string) string { return "/category/" + slug }

// Tag 返回标签页路径。
func Tag(slug string) string { return "/tag/" + slug }
