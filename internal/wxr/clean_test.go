package wxr

import "testing"

func TestCleanContent_Gutenberg(t *testing.T) {
	in := "<!-- wp:paragraph -->\n<p>hello</p>\n<!-- /wp:paragraph -->"
	got := CleanContent(in)
	want := "<p>hello</p>"
	if got != want {
		t.Errorf("CleanContent() = %q, want %q", got, want)
	}
}

func TestCleanContent_KeepsMore(t *testing.T) {
	in := "<p>a</p>\n<!-- wp:more -->\n<!--more-->\n<!-- /wp:more -->\n<p>b</p>"
	got := CleanContent(in)
	if !contains(got, MoreMarker) {
		t.Errorf("CleanContent() should keep <!--more-->, got %q", got)
	}
}

func TestCleanContent_Caption(t *testing.T) {
	in := `[caption id="x" align="alignnone" width="150"]<a href="//youthlin.com/a.png"><img src="//youthlin.com/a-150x150.png"></a> 说明文字[/caption]`
	got := CleanContent(in)
	if !contains(got, "<figure>") || !contains(got, "<figcaption>说明文字</figcaption>") {
		t.Errorf("caption not converted to figure: %q", got)
	}
	if contains(got, "[caption") {
		t.Errorf("caption shortcode残留: %q", got)
	}
	if contains(got, "//youthlin.com") && !contains(got, "https://youthlin.com") {
		t.Errorf("协议相对 URL 未归一化: %q", got)
	}
}

func TestSplitMore(t *testing.T) {
	above, hasMore := SplitMore("<p>开头</p>" + MoreMarker + "<p>剩余</p>")
	if !hasMore || above != "<p>开头</p>" {
		t.Errorf("SplitMore() = (%q, %v)", above, hasMore)
	}
	above2, hasMore2 := SplitMore("<p>无 more</p>")
	if hasMore2 || above2 != "<p>无 more</p>" {
		t.Errorf("SplitMore() no-more = (%q, %v)", above2, hasMore2)
	}
}

func TestRenderDetail(t *testing.T) {
	got := RenderDetail("<p>a</p>"+MoreMarker+"<p>b</p>", 123)
	want := `<p>a</p><span id="more-123"></span><p>b</p>`
	if got != want {
		t.Errorf("RenderDetail() = %q, want %q", got, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
