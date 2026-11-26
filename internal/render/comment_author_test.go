package render

import (
	"strings"
	"testing"

	htmlparser "golang.org/x/net/html"

	"github.com/youthlin/wenlog/internal/model"
)

func TestCommentAuthorWebsiteLinks(t *testing.T) {
	tests := []struct{ name, website, href string }{
		{"https", "https://example.com/blog", "https://example.com/blog"},
		{"http", "http://example.com", "http://example.com"},
		{"whitespace and query", " https://example.com/?a=1&b=2 ", "https://example.com/?a=1&b=2"},
		{"uppercase scheme", "HTTPS://example.com", "https://example.com"},
		{"attribute injection", `https://example.com/?q=" onmouseover="alert(1)`, "https://example.com/?q=\" onmouseover=\"alert(1)"},
		{"empty", "", ""},
		{"relative", "/blog", ""},
		{"scheme relative", "//example.com", ""},
		{"javascript", "javascript:alert(1)", ""},
		{"data", "data:text/html,<script>alert(1)</script>", ""},
		{"missing host", "https:///blog", ""},
		{"malformed host", "https://example.com\\evil", ""},
		{"control character", "https://example.com/\npath", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := map[string]any{"Comments": []model.Comment{
				{ID: 1, Author: `读者 <script>alert("x")</script>`, URL: tt.website, Content: "评论"},
				{ID: 2, ParentID: 1, Author: "回复者", URL: "https://reply.example.com", Content: "回复"},
			}}
			rendered := string(listComments(&RequestContext{Data: data}, data, "ul", 44))
			document, err := htmlparser.Parse(strings.NewReader(rendered))
			if err != nil {
				t.Fatal(err)
			}
			var authors []*htmlparser.Node
			var walk func(*htmlparser.Node)
			walk = func(n *htmlparser.Node) {
				if n.Type == htmlparser.ElementNode {
					if n.Data == "script" {
						t.Fatal("author name produced script element")
					}
					for _, a := range n.Attr {
						if a.Key == "onmouseover" {
							t.Fatal("URL produced event handler attribute")
						}
					}
					if nodeAttr(n, "class") == "comment-author" {
						authors = append(authors, n)
					}
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					walk(child)
				}
			}
			walk(document)
			if len(authors) != 2 {
				t.Fatalf("rendered %d authors, want parent and reply", len(authors))
			}
			parent := authors[0]
			if tt.href == "" {
				if parent.Data != "span" || nodeAttr(parent, "href") != "" {
					t.Fatal("invalid website should render plain name")
				}
			} else {
				if parent.Data != "a" || nodeAttr(parent, "href") != tt.href {
					t.Fatalf("link href=%q, want %q", nodeAttr(parent, "href"), tt.href)
				}
				if nodeAttr(parent, "target") != "_blank" || nodeAttr(parent, "rel") != "ugc nofollow noopener noreferrer" {
					t.Fatal("missing external comment link attributes")
				}
			}
			if parent.FirstChild == nil || parent.FirstChild.Data != `读者 <script>alert("x")</script>` {
				t.Fatal("author text was not preserved")
			}
			if authors[1].Data != "a" || nodeAttr(authors[1], "href") != "https://reply.example.com" {
				t.Fatal("reply author website missing")
			}
		})
	}
}

func nodeAttr(n *htmlparser.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
