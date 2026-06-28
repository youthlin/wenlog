package plugin

import (
	"bytes"
	"context"
	"html"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	root "github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/script"
	gettext "github.com/youthlin/t"
)

// RenderWidget 渲染插件组件。优先执行 widget.render action；如果 action 没有输出，回退到插件 widgets/<id>.gohtml 模板。
func (m *Manager) RenderWidget(ctx context.Context, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool) {
	if m == nil || pluginID == "" || widgetID == "" {
		return "", false
	}
	p := m.Get(pluginID)
	if p == nil {
		return "", false
	}
	if options == nil {
		options = map[string]string{}
	}
	renderCtx := root.WidgetRenderContext{PluginID: pluginID, WidgetID: widgetID, Options: options, Data: data}
	if html, ok := m.renderWidgetByAction(ctx, renderCtx); ok {
		return html, true
	}
	return m.renderWidgetByTemplate(ctx, p, renderCtx)
}

func (m *Manager) renderWidgetByAction(ctx context.Context, renderCtx root.WidgetRenderContext) (template.HTML, bool) {
	hooks := m.Hooks()
	if hooks == nil {
		return "", false
	}
	var out strings.Builder
	hooks.DoAction(ctx, root.HookWidgetRender, &out, renderCtx)
	if out.Len() == 0 {
		return "", false
	}
	return template.HTML(out.String()), true
}

func (m *Manager) renderWidgetByTemplate(ctx context.Context, p *Plugin, renderCtx root.WidgetRenderContext) (template.HTML, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	path := filepath.Join(p.WidgetsDir(), renderCtx.WidgetID+".gohtml")
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	api := m.pluginAPI(ctx, p)
	tpl, err := template.New(filepath.Base(path)).Funcs(pluginTemplateFuncs(ctx, renderCtx.Options, api)).ParseFiles(path)
	if err != nil {
		if m.log != nil {
			m.log.Warn("解析插件组件模板失败", "plugin", p.ID, "widget", renderCtx.WidgetID, "path", path, "error", err)
		}
		return "", false
	}
	tr := gettext.WithContext(ctx).D(p.PluginDomain())
	data := map[string]any{
		"PluginID": renderCtx.PluginID,
		"WidgetID": renderCtx.WidgetID,
		"Options":  renderCtx.Options,
		"Data":     renderCtx.Data,
		"tp":       tr,
		"api":      api,
	}
	for _, name := range []string{"widget_" + renderCtx.WidgetID, filepath.Base(path)} {
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, name, data); err == nil {
			return template.HTML(buf.String()), true
		}
	}
	return "", false
}

func (m *Manager) pluginAPI(ctx context.Context, p *Plugin) *root.API {
	if p == nil {
		return root.New(nil, nil, "").WithContext(ctx)
	}
	m.mu.RLock()
	script := m.scripts[p.ID]
	m.mu.RUnlock()
	if script != nil && script.api != nil {
		return script.api.WithContext(ctx)
	}
	return root.New(nil, nil, p.PluginDomain()).WithContext(ctx)
}

func pluginTemplateFuncs(ctx context.Context, options map[string]string, api *root.API) template.FuncMap {
	return template.FuncMap{
		"pluginOption": func(key string) string { return options[key] },
		"option":       func(key string) string { return options[key] },
		"hookInvoke": func(name string, args ...any) any {
			if api == nil {
				return nil
			}
			return api.InvokeFunc(ctx, name, script.ParseKVArgs(args))
		},
		"pluginData": func(name string, args ...any) any {
			if api == nil {
				return nil
			}
			return api.InvokeFunc(ctx, name, script.ParseKVArgs(args))
		},
		"safeHTML":   func(s string) template.HTML { return template.HTML(s) },
		"escapeHTML": html.EscapeString,
		"toInt":      func(s string) int { n, _ := strconv.Atoi(s); return n },
		"default": func(def, val any) any {
			if val == nil {
				return def
			}
			rv := reflect.ValueOf(val)
			if !rv.IsValid() || rv.IsZero() {
				return def
			}
			return val
		},
	}
}

// WidgetsDir 返回插件组件模板目录路径。
func (p *Plugin) WidgetsDir() string {
	if p == nil {
		return ""
	}
	return filepath.Join(p.Dir, "widgets")
}

// WriteString 是插件 action 可选使用的 HTML 输出辅助。
func WriteString(w io.StringWriter, s string) {
	if w != nil {
		_, _ = w.WriteString(s)
	}
}
