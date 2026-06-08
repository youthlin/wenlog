package render

import "testing"

func TestGravatar(t *testing.T) {
	// 已知:md5("me@example.com") = ... ;只校验前缀与不随大小写/空白变化。
	a := gravatar("Me@Example.com")
	b := gravatar("  me@example.com ")
	if a != b {
		t.Errorf("gravatar should normalize case/space: %q != %q", a, b)
	}
	if got := gravatar("me@example.com"); len(got) < len("https://cn.cravatar.com/avatar/")+32 {
		t.Errorf("gravatar url too short: %q", got)
	}
}
