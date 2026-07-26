package handler

import (
	"encoding/xml"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/permalink"
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
	filterCtx := c.Request.Context()
	if loader, err := h.st.LoadAllCached(c); err == nil && loader != nil {
		filterCtx = hook.WithDataLoader(filterCtx, loader)
	}
	ch := rssChan{
		Title:       s.SiteName,
		Link:        baseURL,
		Description: s.SiteName,
	}
	for i := range res.Posts {
		p := &res.Posts[i]
		link := baseURL + permalink.Post(p)
		title := h.renderer.FilterPostTitle(filterCtx, p)
		desc := string(h.renderer.FilterPostContent(filterCtx, p))
		if desc == "" {
			desc = title
		}
		ch.Items = append(ch.Items, rssItem{
			Title:   title,
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
		h.log.ErrorContext(c, "feed xml encode", "error", err)
	}
}

// atomFeed 是 Atom 1.0 文档结构(RFC 4287)。
type atomFeed struct {
	XMLName  xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle,omitempty"`
	ID       string      `xml:"id"`
	Updated  string      `xml:"updated"`
	Link     []atomLink  `xml:"link"`
	Entries  []atomEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
}

type atomEntry struct {
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Link    atomLink    `xml:"link"`
	Content atomContent `xml:"content"`
	Author  atomAuthor  `xml:"author"`
}

type atomContent struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

// AtomFeed 输出 Atom 1.0 feed。
func (h *Public) AtomFeed(c *gin.Context) {
	syncPostPermalink(c, h.st)
	s := h.loadSettings(c)
	res, err := h.st.ListPosts(c, 1, s.FeedSize, "", "")
	if err != nil {
		h.serverError(c, err)
		return
	}
	baseURL := requestBaseURL(c)
	atomURL := baseURL + "/atom"
	filterCtx := c.Request.Context()
	if loader, err := h.st.LoadAllCached(c); err == nil && loader != nil {
		filterCtx = hook.WithDataLoader(filterCtx, loader)
	}

	feed := atomFeed{
		Title:    s.SiteName,
		Subtitle: s.SiteDescription,
		ID:       baseURL + "/",
		Link: []atomLink{
			{Href: atomURL, Rel: "self"},
			{Href: baseURL + "/", Rel: "alternate"},
		},
	}

	for i := range res.Posts {
		p := &res.Posts[i]
		link := baseURL + permalink.Post(p)
		title := h.renderer.FilterPostTitle(filterCtx, p)
		content := string(h.renderer.FilterPostContent(filterCtx, p))
		if content == "" {
			content = title
		}
		authorName := ""
		if p.Author.DisplayName != "" {
			authorName = p.Author.DisplayName
		} else if p.Author.Username != "" {
			authorName = p.Author.Username
		}
		feed.Entries = append(feed.Entries, atomEntry{
			Title:   title,
			ID:      link,
			Updated: p.PublishedAt.Format(time.RFC3339),
			Link:    atomLink{Href: link, Rel: "alternate"},
			Content: atomContent{Type: "html", Body: content},
			Author:  atomAuthor{Name: authorName},
		})
	}

	// 用最新文章的发布时间作为 feed 的 updated。
	if len(res.Posts) > 0 {
		feed.Updated = res.Posts[0].PublishedAt.Format(time.RFC3339)
	}

	c.Header("Content-Type", "application/atom+xml; charset=utf-8")
	c.String(http.StatusOK, xml.Header)
	enc := xml.NewEncoder(c.Writer)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		h.log.ErrorContext(c, "atom feed xml encode", "error", err)
	}
}
