package util

import (
	"strings"
	"testing"
)

func TestGenerateRandomString(t *testing.T) {
	t.Run("default length", func(t *testing.T) {
		s := GenerateRandomString(16)
		if len(s) != 16 {
			t.Errorf("GenerateRandomString(16) length = %d, want 16", len(s))
		}
	})

	t.Run("zero length", func(t *testing.T) {
		s := GenerateRandomString(0)
		if len(s) != 0 {
			t.Errorf("GenerateRandomString(0) length = %d, want 0", len(s))
		}
	})

	t.Run("with custom letters", func(t *testing.T) {
		letters := "abc"
		s := GenerateRandomString(100, WithLetters(letters))
		if len(s) != 100 {
			t.Errorf("length = %d, want 100", len(s))
		}
		for _, r := range s {
			if !strings.ContainsRune(letters, r) {
				t.Errorf("unexpected char %q in result %q", r, s)
			}
		}
	})

	t.Run("with alpha numer", func(t *testing.T) {
		s := GenerateRandomString(50, WithAlphaNumer())
		if len(s) != 50 {
			t.Errorf("length = %d, want 50", len(s))
		}
		alphaNum := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		for _, r := range s {
			if !strings.ContainsRune(alphaNum, r) {
				t.Errorf("unexpected char %q in result %q", r, s)
			}
		}
	})

	t.Run("uniqueness", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			s := GenerateRandomString(16)
			if seen[s] {
				t.Errorf("duplicate random string: %q", s)
			}
			seen[s] = true
		}
	})
}

func TestWithLetters(t *testing.T) {
	opt := WithLetters("xyz")
	o := &generateOpt{}
	opt(o)
	if o.letters != "xyz" {
		t.Errorf("WithLetters = %q, want %q", o.letters, "xyz")
	}
}

func TestWithAlphaNumer(t *testing.T) {
	opt := WithAlphaNumer()
	o := &generateOpt{}
	opt(o)
	if o.letters != "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789" {
		t.Errorf("WithAlphaNumer = %q", o.letters)
	}
}
