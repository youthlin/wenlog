package handler

import (
	"encoding/xml"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/permalink"
)

// rss 是 RSS 2.0 文档结构。
type rss struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel rssChan  `xml:"channel"`
}

type rssChan struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	GUID    string `xml:"guid"`
	PubDate string `xml:"pubDate"`
	Desc    string `xml:"description"`
}

// Feed 输出 RSS,保留 WordPress 的 /feed 链接。
func (h *Public) Feed(c *gin.Context) {
	syncPostPermalink(c, h.st)
	s := h.loadSettings(c)
	res, err := h.st.ListPosts(c, 1, s.FeedSize, "", "")
	if err != nil {
		h.serverError(c, err)
		return
	}
	baseURL := requestBaseURL(c)
	ch := rssChan{
		Title:       s.SiteName,
		Link:        baseURL,
		Description: s.SiteName,
	}
	for i := range res.Posts {
		p := &res.Posts[i]
		link := baseURL + permalink.Post(p)
		desc := p.Excerpt
		if desc == "" {
			desc = p.Title
		}
		ch.Items = append(ch.Items, rssItem{
			Title:   p.Title,
			Link:    link,
			GUID:    link,
			PubDate: p.PublishedAt.Format(time.RFC1123Z),
			Desc:    desc,
		})
	}
	doc := rss{Version: "2.0", Channel: ch}
	c.Header("Content-Type", "application/rss+xml; charset=utf-8")
	c.String(http.StatusOK, xml.Header)
	enc := xml.NewEncoder(c.Writer)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		h.log.Error("feed xml encode", "error", err)
	}
}
