package handler

import (
	"encoding/xml"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/permalink"
)

// sitemapindex 是 sitemap 索引文件结构。
type sitemapindex struct {
	XMLName xml.Name  `xml:"urlset"`
	Xmlns   string    `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

// Sitemap 输出 XML Sitemap，包含所有已发布文章和页面。
func (h *Public) Sitemap(c *gin.Context) {
	syncPostPermalink(c, h.st)
	loader, err := h.st.LoadAllCached(c)
	if err != nil {
		h.serverError(c, err)
		return
	}

	baseURL := requestBaseURL(c)
	doc := sitemapindex{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}

	// 首页
	doc.URLs = append(doc.URLs, sitemapURL{
		Loc:        baseURL + "/",
		ChangeFreq: "daily",
		Priority:   "1.0",
	})

	// 所有已发布文章
	posts := loader.PostsByType("post")
	for _, p := range posts {
		doc.URLs = append(doc.URLs, sitemapURL{
			Loc:        baseURL + permalink.Post(p),
			LastMod:    p.ModifiedAt.Format(time.RFC3339),
			ChangeFreq: "monthly",
			Priority:   "0.8",
		})
	}

	// 所有已发布页面
	pages := loader.PostsByType("page")
	for _, p := range pages {
		doc.URLs = append(doc.URLs, sitemapURL{
			Loc:        baseURL + permalink.Page(p),
			LastMod:    p.ModifiedAt.Format(time.RFC3339),
			ChangeFreq: "monthly",
			Priority:   "0.6",
		})
	}

	// 分类页
	for _, cat := range loader.AllCategories() {
		doc.URLs = append(doc.URLs, sitemapURL{
			Loc:        baseURL + permalink.Category(cat.Slug),
			ChangeFreq: "weekly",
			Priority:   "0.5",
		})
	}

	// 标签页
	for _, tag := range loader.AllTags() {
		doc.URLs = append(doc.URLs, sitemapURL{
			Loc:        baseURL + permalink.Tag(tag.Slug),
			ChangeFreq: "weekly",
			Priority:   "0.4",
		})
	}

	// 归档页
	doc.URLs = append(doc.URLs, sitemapURL{
		Loc:        baseURL + "/archive",
		ChangeFreq: "monthly",
		Priority:   "0.3",
	})

	c.Header("Content-Type", "application/xml; charset=utf-8")
	c.String(http.StatusOK, xml.Header)
	enc := xml.NewEncoder(c.Writer)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil && h.log != nil {
		h.log.Error("sitemap xml encode", "error", err)
	}
}
