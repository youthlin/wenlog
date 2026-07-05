package hook

import (
	"context"
	"html/template"
	"io"
	"strings"
	"testing"
)

// ========== Args 测试 ==========

func TestArgs_Any(t *testing.T) {
	a := Args{"key": "value", "num": 42}
	if got := a.Any("key"); got != "value" {
		t.Errorf("Any(key) = %v, want value", got)
	}
	if got := a.Any("missing"); got != nil {
		t.Errorf("Any(missing) = %v, want nil", got)
	}
}

func TestArgs_String(t *testing.T) {
	a := Args{"name": "hello", "empty": "", "num": 42}
	tests := []struct {
		key, def, want string
	}{
		{"name", "fallback", "hello"},
		{"empty", "fallback", "fallback"},
		{"missing", "fallback", "fallback"},
		{"num", "fallback", "fallback"}, // 非 string 类型回退
	}
	for _, tt := range tests {
		if got := a.String(tt.key, tt.def); got != tt.want {
			t.Errorf("String(%q, %q) = %q, want %q", tt.key, tt.def, got, tt.want)
		}
	}
}

func TestArgs_Int(t *testing.T) {
	a := Args{
		"int":    42,
		"int64":  int64(100),
		"uint":   uint(7),
		"float":  3.14,
		"str":    "99",
		"badstr": "abc",
		"empty":  "",
	}
	tests := []struct {
		key  string
		def  int
		want int
	}{
		{"int", 0, 42},
		{"int64", 0, 100},
		{"uint", 0, 7},
		{"float", 0, 3},
		{"str", 0, 99},
		{"badstr", 5, 5},
		{"empty", 5, 5},
		{"missing", 10, 10},
	}
	for _, tt := range tests {
		if got := a.Int(tt.key, tt.def); got != tt.want {
			t.Errorf("Int(%q, %d) = %d, want %d", tt.key, tt.def, got, tt.want)
		}
	}
}

func TestArgs_PositiveInt(t *testing.T) {
	a := Args{"pos": 5, "zero": 0, "neg": -1, "str": "10"}
	tests := []struct {
		key  string
		def  int
		want int
	}{
		{"pos", 1, 5},
		{"zero", 1, 1},
		{"neg", 1, 1},
		{"str", 1, 10},
		{"missing", 3, 3},
	}
	for _, tt := range tests {
		if got := a.PositiveInt(tt.key, tt.def); got != tt.want {
			t.Errorf("PositiveInt(%q, %d) = %d, want %d", tt.key, tt.def, got, tt.want)
		}
	}
}

func TestArgs_Bool(t *testing.T) {
	a := Args{
		"btrue":   true,
		"bfalse":  false,
		"strue":   "true",
		"s1":      "1",
		"sfalse":  "false",
		"s0":      "0",
		"sbad":    "invalid",
		"nonbool": 42,
	}
	tests := []struct {
		key  string
		def  bool
		want bool
	}{
		{"btrue", false, true},
		{"bfalse", true, false},
		{"strue", false, true},
		{"s1", false, true},
		{"sfalse", true, false},
		{"s0", true, false},
		{"sbad", true, true},
		{"sbad", false, false},
		{"nonbool", true, true},
		{"missing", true, true},
	}
	for _, tt := range tests {
		if got := a.Bool(tt.key, tt.def); got != tt.want {
			t.Errorf("Bool(%q, %v) = %v, want %v", tt.key, tt.def, got, tt.want)
		}
	}
}

// ========== Context 函数测试 ==========

func TestWithActionWriter(t *testing.T) {
	var buf strings.Builder
	ctx := WithActionWriter(context.Background(), &buf)
	w := getActionWriter(ctx)
	if w == nil {
		t.Fatal("getActionWriter returned nil")
	}
	w.WriteString("hello")
	if buf.String() != "hello" {
		t.Errorf("writer content = %q, want hello", buf.String())
	}
}

func TestWithActionWriter_NilWriter(t *testing.T) {
	ctx := context.WithValue(context.Background(), actionWriterKey{}, "not a writer")
	w := getActionWriter(ctx)
	if w != nil {
		t.Error("getActionWriter should return nil for non-StringWriter value")
	}
}

func TestWithCurrentHook(t *testing.T) {
	ctx := WithCurrentHook(context.Background(), "test.hook")
	if got := CurrentHook(ctx); got != "test.hook" {
		t.Errorf("CurrentHook = %q, want test.hook", got)
	}
}

func TestCurrentHook_Empty(t *testing.T) {
	if got := CurrentHook(context.Background()); got != "" {
		t.Errorf("CurrentHook = %q, want empty", got)
	}
}

func TestWithDataLoader(t *testing.T) {
	ctx := WithDataLoader(nil, nil)
	if ctx == nil {
		t.Error("WithDataLoader(nil, nil) should return non-nil context")
	}
	// nil loader should not be stored
	ctx = WithDataLoader(context.Background(), nil)
	if ctx == nil {
		t.Error("WithDataLoader should return non-nil context")
	}
}

// ========== Source 测试 ==========

func TestSource(t *testing.T) {
	s := Source{Type: "plugin", ID: "test-plugin"}
	if s.Type != "plugin" || s.ID != "test-plugin" {
		t.Errorf("Source = %+v", s)
	}
}

// ========== WidgetDecl / WidgetInstance 测试 ==========

func TestWidgetDecl(t *testing.T) {
	d := WidgetDecl{
		ID:       "search",
		Label:    "搜索",
		Source:   "builtin",
		PluginID: "",
	}
	if d.ID != "search" {
		t.Errorf("WidgetDecl.ID = %q, want search", d.ID)
	}
}

func TestWidgetInstance(t *testing.T) {
	inst := WidgetInstance{
		InstanceID: "inst-1",
		WidgetID:   "search",
		Settings:   map[string]string{"title": "搜索"},
	}
	if inst.InstanceID != "inst-1" {
		t.Errorf("InstanceID = %q", inst.InstanceID)
	}
	if inst.Settings["title"] != "搜索" {
		t.Errorf("Settings[title] = %q", inst.Settings["title"])
	}
}

// ========== OptionDecl 测试 ==========

func TestOptionDecl(t *testing.T) {
	minVal := 1.0
	maxVal := 100.0
	opt := OptionDecl{
		ID:      "posts_per_page",
		Type:    "number",
		Label:   "每页文章数",
		Default: "10",
		Min:     &minVal,
		Max:     &maxVal,
		Options: []SelectOpt{{Value: "5", Label: "5篇"}, {Value: "10", Label: "10篇"}},
	}
	if opt.ID != "posts_per_page" {
		t.Errorf("ID = %q", opt.ID)
	}
	if *opt.Min != 1.0 {
		t.Errorf("Min = %v", *opt.Min)
	}
	if len(opt.Options) != 2 {
		t.Errorf("Options length = %d, want 2", len(opt.Options))
	}
}

// ========== PostView 测试 ==========

func TestPostView_PostURLFields(t *testing.T) {
	p := PostView{ID: 123, Title: "Test", Slug: "test-slug", PostType: "post"}
	fields := p.PostURLFields()
	if fields.ID != 123 {
		t.Errorf("ID = %d, want 123", fields.ID)
	}
	if fields.Title != "Test" {
		t.Errorf("Title = %q, want Test", fields.Title)
	}
	if fields.Slug != "test-slug" {
		t.Errorf("Slug = %q, want test-slug", fields.Slug)
	}
	if fields.PostType != "post" {
		t.Errorf("PostType = %q, want post", fields.PostType)
	}
}

// ========== WidgetRegistry 测试 ==========

type mockWidget struct {
	decl WidgetDecl
}

func (m *mockWidget) Meta() WidgetDecl { return m.decl }
func (m *mockWidget) Render(_ context.Context, _ *template.Template, _ WidgetInstance, _ any) (template.HTML, error) {
	return "", nil
}

func TestWidgetRegistry_RegisterAndGet(t *testing.T) {
	r := NewWidgetRegistry()
	w := &mockWidget{decl: WidgetDecl{ID: "search", Source: "builtin"}}
	r.Register(w)

	got := r.Get("builtin", "search", "")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Meta().ID != "search" {
		t.Errorf("Meta().ID = %q, want search", got.Meta().ID)
	}
}

func TestWidgetRegistry_GetMissing(t *testing.T) {
	r := NewWidgetRegistry()
	if got := r.Get("builtin", "nonexistent", ""); got != nil {
		t.Error("Get should return nil for unregistered widget")
	}
}

func TestWidgetRegistry_PluginWidget(t *testing.T) {
	r := NewWidgetRegistry()
	w := &mockWidget{decl: WidgetDecl{ID: "popular_posts", Source: "plugin", PluginID: "common-widgets"}}
	r.Register(w)

	got := r.Get("plugin", "popular_posts", "common-widgets")
	if got == nil {
		t.Fatal("Get returned nil for plugin widget")
	}
	if got.Meta().PluginID != "common-widgets" {
		t.Errorf("PluginID = %q", got.Meta().PluginID)
	}
}

func TestWidgetRegistry_NilReceiver(t *testing.T) {
	var r *WidgetRegistry
	r.Register(&mockWidget{decl: WidgetDecl{ID: "x", Source: "builtin"}}) // 不应 panic
	if got := r.Get("builtin", "x", ""); got != nil {
		t.Error("Get on nil registry should return nil")
	}
}

func TestWidgetRegistry_NilWidget(t *testing.T) {
	r := NewWidgetRegistry()
	r.Register(nil) // 不应 panic
	if got := r.Get("builtin", "x", ""); got != nil {
		t.Error("nil widget should not be registered")
	}
}

// ========== widgetDeclKey 测试 ==========

func TestWidgetDeclKey(t *testing.T) {
	tests := []struct {
		source, id, pluginID, want string
	}{
		{"builtin", "search", "", "builtin:search"},
		{"theme", "popular_posts", "", "theme:popular_posts"},
		{"plugin", "saying", "common-widgets", "plugin:common-widgets:saying"},
		{"plugin", "saying", "", "plugin:saying"},
	}
	for _, tt := range tests {
		if got := widgetDeclKey(tt.source, tt.id, tt.pluginID); got != tt.want {
			t.Errorf("widgetDeclKey(%q, %q, %q) = %q, want %q", tt.source, tt.id, tt.pluginID, got, tt.want)
		}
	}
}

// ========== Hook 常量测试 ==========

func TestHookConstants(t *testing.T) {
	// 确保所有 hook 名非空
	hooks := []string{
		HookHeadEnd, HookBodyEnd, HookCommentFormAfterTextarea,
		HookWidgetRender, HookPostTitle, HookPostExcerptHTML,
		HookPostContentHTML, HookCommentContentHTML, HookWidgetRenderHTML,
		HookHeadMeta,
	}
	for _, h := range hooks {
		if h == "" {
			t.Error("hook constant should not be empty")
		}
	}
}

// ========== Priority 常量测试 ==========

func TestPriorityOrder(t *testing.T) {
	if PriorityEarly >= PriorityDefault {
		t.Error("PriorityEarly should be less than PriorityDefault")
	}
	if PriorityDefault >= PriorityLate {
		t.Error("PriorityDefault should be less than PriorityLate")
	}
}

// ========== HeadEndData / BodyEndData 测试 ==========

func TestHeadEndData(t *testing.T) {
	d := HeadEndData{Data: map[string]string{"key": "value"}}
	if d.Data == nil {
		t.Error("HeadEndData.Data should not be nil")
	}
}

func TestBodyEndData(t *testing.T) {
	d := BodyEndData{Data: "test"}
	if d.Data != "test" {
		t.Errorf("BodyEndData.Data = %v", d.Data)
	}
}

// ========== CommentFormAfterTextareaData 测试 ==========

func TestCommentFormAfterTextareaData(t *testing.T) {
	d := CommentFormAfterTextareaData{Data: 42}
	if d.Data != 42 {
		t.Errorf("Data = %v", d.Data)
	}
}

// ========== WidgetRenderContext 测试 ==========

func TestWidgetRenderContext(t *testing.T) {
	ctx := WidgetRenderContext{
		PluginID: "test-plugin",
		WidgetID: "test-widget",
		Options:  map[string]string{"key": "val"},
		Data:     "data",
	}
	if ctx.PluginID != "test-plugin" {
		t.Errorf("PluginID = %q", ctx.PluginID)
	}
}

// ========== SelectOpt 测试 ==========

func TestSelectOpt(t *testing.T) {
	opt := SelectOpt{Value: "en", Label: "English"}
	if opt.Value != "en" || opt.Label != "English" {
		t.Errorf("SelectOpt = %+v", opt)
	}
}

// ========== API 构造测试 ==========

func TestNew(t *testing.T) {
	var addActionCalled, addFilterCalled bool
	addAction := func(name string, fn any, priority ...int) { addActionCalled = true }
	addFilter := func(name string, fn any, priority ...int) { addFilterCalled = true }
	api := New(addAction, addFilter, "test-domain")
	if api == nil {
		t.Fatal("New returned nil")
	}
	if api.domain != "test-domain" {
		t.Errorf("domain = %q", api.domain)
	}
	if api.funcs == nil {
		t.Error("funcs map should be initialized")
	}
	// 验证闭包正确绑定
	api.addAction("test", func() {})
	if !addActionCalled {
		t.Error("addAction was not called")
	}
	api.addFilter("test", func() {})
	if !addFilterCalled {
		t.Error("addFilter was not called")
	}
}

// ========== io.StringWriter 接口验证 ==========

func TestActionWriterInterface(t *testing.T) {
	var buf strings.Builder
	ctx := WithActionWriter(context.Background(), &buf)
	w := getActionWriter(ctx)
	if w == nil {
		t.Fatal("getActionWriter returned nil")
	}
	// 验证 io.StringWriter 接口
	var sw io.StringWriter = w
	n, err := sw.WriteString("test")
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("WriteString wrote %d bytes, want 4", n)
	}
	if buf.String() != "test" {
		t.Errorf("buffer = %q, want test", buf.String())
	}
}
