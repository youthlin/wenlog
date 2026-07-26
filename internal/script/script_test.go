package script

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCompileAndRegisterHonorsContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := CompileAndRegister(ctx, `package plugin

func Register() {
	for {
	}
}
`, CompileOptions{
		Subject:     "插件",
		PackageName: "plugin",
	})
	if err == nil {
		t.Fatal("CompileAndRegister should fail on Register timeout")
	}
	if !strings.Contains(err.Error(), "执行超时") {
		t.Fatalf("CompileAndRegister error = %v, want timeout", err)
	}
}
