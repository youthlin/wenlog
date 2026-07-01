package plugin

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/youthlin/blog/hook"
	"github.com/youthlin/blog/internal/render"
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
		slog.Info("RenderWidget: plugin not found", "plugin_id", pluginID)
		return "", false
	}
	if options == nil {
		options = map[string]string{}
	}
	slog.Info("RenderWidget: rendering plugin widget", "plugin_id", pluginID, "widget_id", widgetID)
	renderCtx := hook.WidgetRenderContext{PluginID: pluginID, WidgetID: widgetID, Options: options, Data: data}
	if html, ok := m.renderWidgetByAction(ctx, renderCtx); ok {
		slog.Info("RenderWidget: rendered by action", "plugin_id", pluginID, "widget_id", widgetID)
		return html, true
	}
	return m.renderWidgetByTemplate(ctx, p, renderCtx)
}

// pluginWidget 是插件组件的 Widget 接口实现，组合了 action 和 template 两种渲染模式。
type pluginWidget struct {
	m        *Manager
	decl     hook.WidgetDecl
	pluginID string
}

func (w *pluginWidget) Meta() hook.WidgetDecl { return w.decl }

func (w *pluginWidget) Render(ctx context.Context, tpl *template.Template, instance hook.WidgetInstance, data any) (template.HTML, error) {
	html, ok := w.m.RenderWidget(ctx, w.pluginID, w.decl.ID, instance.Settings, data)
	if !ok {
		return "", nil
	}
	return html, nil
}

// RegisterPluginWidgets 将已启用插件的组件注册到组件注册表。
func (m *Manager) RegisterPluginWidgets(ctx context.Context, widgetRegistry *hook.WidgetRegistry) {
	if m == nil || widgetRegistry == nil {
		return
	}
	for _, p := range m.Enabled(ctx) {
		for _, decl := range p.Widgets {
			if decl.ID == "" {
				continue
			}
			d := decl
			d.Source = "plugin"
			d.PluginID = p.ID
			w := &pluginWidget{m: m, decl: d, pluginID: p.ID}
			widgetRegistry.Register(w)
			slog.Info("RegisterPluginWidgets: registered", "plugin", p.ID, "widget", d.ID)
		}
	}
}

func (m *Manager) renderWidgetByAction(ctx context.Context, renderCtx hook.WidgetRenderContext) (template.HTML, bool) {
	hooks := m.GetRegistry()
	if hooks == nil {
		return "", false
	}
	var out strings.Builder
	hooks.DoAction(ctx, hook.HookWidgetRender, &out, renderCtx)
	if out.Len() == 0 {
		return "", false
	}
	return template.HTML(out.String()), true
}

func (m *Manager) renderWidgetByTemplate(ctx context.Context, p *Plugin, renderCtx hook.WidgetRenderContext) (template.HTML, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	path := filepath.Join(p.WidgetsDir(), renderCtx.WidgetID+".gohtml")
	if _, err := os.Stat(path); err != nil {
		slog.Info("renderWidgetByTemplate: template file not found", "plugin", p.ID, "widget", renderCtx.WidgetID, "path", path, "error", err)
		return "", false
	}
	api := m.pluginAPI(ctx, p)
	slog.Info("renderWidgetByTemplate: got plugin API", "plugin", p.ID, "widget", renderCtx.WidgetID, "api_has_funcs", api != nil && len(api.FuncNames()) > 0)
	tpl, err := template.New(filepath.Base(path)).
		Funcs(pluginTemplateFuncs(ctx, renderCtx.Options, api)).
		ParseFiles(path)
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
			slog.Info("renderWidgetByTemplate: template executed successfully", "plugin", p.ID, "widget", renderCtx.WidgetID, "html_len", buf.Len())
			return template.HTML(buf.String()), true
		}
		slog.Info("renderWidgetByTemplate: template execute failed", "plugin", p.ID, "widget", renderCtx.WidgetID, "name", name, "error", err)
	}
	slog.Info("renderWidgetByTemplate: all template names failed", "plugin", p.ID, "widget", renderCtx.WidgetID)
	return "", false
}

func (m *Manager) pluginAPI(ctx context.Context, p *Plugin) *hook.API {
	if p == nil {
		return hook.New(nil, nil, "").WithContext(ctx)
	}
	m.mu.RLock()
	script := m.scripts[p.ID]
	m.mu.RUnlock()
	if script != nil && script.api != nil {
		api := script.api.WithContext(ctx)
		slog.Info("pluginAPI: found script api", "plugin", p.ID, "funcs", api.FuncNames())
		return api
	}
	slog.Info("pluginAPI: no script/api, returning empty API", "plugin", p.ID, "has_script", script != nil, "has_api", script != nil && script.api != nil)
	return hook.New(nil, nil, p.PluginDomain()).WithContext(ctx)
}

func pluginTemplateFuncs(ctx context.Context, options map[string]string, api *hook.API) template.FuncMap {
	funcs := render.CommonFuncMap()
	maps.Copy(funcs, template.FuncMap{
		"pluginOption": func(key string) string { return options[key] },
		"hookInvoke": func(name string, args ...any) any {
			if api == nil {
				slog.Info("hookInvoke: api is nil", "name", name)
				return nil
			}
			result := api.InvokeFunc(ctx, name, script.ParseKVArgs(args))
			slog.Info("hookInvoke: called", "name", name, "result_nil", result == nil)
			return result
		},
	})
	return funcs
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
