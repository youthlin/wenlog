package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{"empty", nil, ""},
		{"single non-empty", []string{"hello"}, "hello"},
		{"first empty then non-empty", []string{"", "  ", "hello"}, "hello"},
		{"all empty", []string{"", "  ", "\t"}, ""},
		{"whitespace only", []string{"  \t"}, ""}, // TrimSpace 后为空
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstNonEmpty(tt.values...); got != tt.want {
				t.Errorf("FirstNonEmpty(%v) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}

func TestFirstNonEmptyOr(t *testing.T) {
	tests := []struct {
		name     string
		fallback string
		values   []string
		want     string
	}{
		{"all empty returns fallback", "default", []string{"", "  "}, "default"},
		{"first non-empty wins", "default", []string{"", "hello"}, "hello"},
		{"no values returns fallback", "default", nil, "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstNonEmptyOr(tt.fallback, tt.values...); got != tt.want {
				t.Errorf("FirstNonEmptyOr(%q, %v) = %q, want %q", tt.fallback, tt.values, got, tt.want)
			}
		})
	}
}

func TestNormalizeDefaultAvatar(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"mp", "mp"},
		{"MP", "mp"},
		{"  Mp  ", "mp"},
		{"blank", "blank"},
		{"cravatar", "cravatar"},
		{"identicon", "identicon"},
		{"wavatar", "wavatar"},
		{"monsterid", "monsterid"},
		{"retro", "retro"},
		{"robohash", "robohash"},
		{"invalid", "mp"}, // 默认值
		{"", "mp"},
		{"unknown", "mp"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := NormalizeDefaultAvatar(tt.input); got != tt.want {
				t.Errorf("NormalizeDefaultAvatar(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPathExists(t *testing.T) {
	// 临时目录肯定存在
	if !PathExists(t.TempDir()) {
		t.Error("PathExists should return true for existing directory")
	}
	// 不存在的路径
	if PathExists(filepath.Join(t.TempDir(), "nonexistent")) {
		t.Error("PathExists should return false for nonexistent path")
	}
	// 临时文件
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if !PathExists(tmpFile) {
		t.Error("PathExists should return true for existing file")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"  Hello  World  ", "hello--world"}, // 连续空格产生连续连字符
		{"Go语言", "go语言"},
		{"hello_world", "hello_world"},
		{"a-b_c", "a-b_c"},
		{"!@#$%^&*()", "!@#$%^&*()"}, // 全特殊字符回退到原字符串
		{"   ", ""},
		{"", ""},
		{"Café", "café"},
		{"你好，世界！", "你好世界"},
		{"UPPERCASE", "uppercase"},
		{"mixed-CASE_example", "mixed-case_example"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := Slugify(tt.input); got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestURLSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"你好世界", "%e4%bd%a0%e5%a5%bd%e4%b8%96%e7%95%8c"},
		{"already%20encoded", "already-encoded"},
		{"a b", "a-b"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := URLSlugify(tt.input); got != tt.want {
				t.Errorf("URLSlugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
