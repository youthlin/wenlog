package plugin

import (
	"bytes"
	"context"
	"html/template"
	"log/slog"
	"maps"
	"os"
	"path/filepath"

	gettext "github.com/youthlin/t"
	"github.com/youthlin/wenlog/hook"
	"github.com/youthlin/wenlog/internal/render"
	"github.com/youthlin/wenlog/internal/script"
)

type widgetRenderContext struct {
	PluginID string
	WidgetID string
	Options  map[string]string
	Data     any
}

// RenderWidget 渲染插件组件。插件组件由 manifest 声明并注册为明确的 Widget renderer，
// 渲染时执行插件 widgets/<id>.gohtml 模板。
func (m *Manager) RenderWidget(ctx context.Context, pluginID, widgetID string, options map[string]string, data any) (template.HTML, bool) {
	if m == nil || pluginID == "" || widgetID == "" {
		return "", false
	}
	p := m.Get(pluginID)
	if p == nil {
		slog.DebugContext(ctx, "RenderWidget: plugin not found", "plugin_id", pluginID)
		return "", false
	}
	if options == nil {
		options = map[string]string{}
	}
	slog.DebugContext(ctx, "RenderWidget: rendering plugin widget", "plugin_id", pluginID, "widget_id", widgetID)
	renderCtx := widgetRenderContext{PluginID: pluginID, WidgetID: widgetID, Options: options, Data: data}
	return m.renderWidgetByTemplate(ctx, p, renderCtx)
}

// pluginWidget 是插件组件的 Widget 接口实现，负责将插件 manifest 中声明的组件绑定到模板 renderer。
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
			slog.DebugContext(ctx, "RegisterPluginWidgets: registered", "plugin", p.ID, "widget", d.ID)
		}
	}
}

func (m *Manager) renderWidgetByTemplate(ctx context.Context, p *Plugin, renderCtx widgetRenderContext) (template.HTML, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	path := filepath.Join(p.WidgetsDir(), renderCtx.WidgetID+".gohtml")
	if _, err := os.Stat(path); err != nil {
		slog.DebugContext(ctx, "renderWidgetByTemplate: template file not found", "plugin", p.ID, "widget", renderCtx.WidgetID, "path", path, "error", err)
		return "", false
	}
	api := m.pluginAPI(ctx, p)
	slog.DebugContext(ctx, "renderWidgetByTemplate: got plugin API", "plugin", p.ID, "widget", renderCtx.WidgetID, "api_has_funcs", api != nil && len(api.FuncNames()) > 0)
	tpl, err := template.New(filepath.Base(path)).
		Funcs(pluginTemplateFuncs(ctx, renderCtx.Options, api)).
		ParseFiles(path)
	if err != nil {
		if m.log != nil {
			m.log.WarnContext(ctx, "解析插件组件模板失败", "plugin", p.ID, "widget", renderCtx.WidgetID, "path", path, "error", err)
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
			slog.DebugContext(ctx, "renderWidgetByTemplate: template executed successfully", "plugin", p.ID, "widget", renderCtx.WidgetID, "html_len", buf.Len())
			return template.HTML(buf.String()), true
		}
		slog.DebugContext(ctx, "renderWidgetByTemplate: template execute failed", "plugin", p.ID, "widget", renderCtx.WidgetID, "name", name, "error", err)
	}
	slog.DebugContext(ctx, "renderWidgetByTemplate: all template names failed", "plugin", p.ID, "widget", renderCtx.WidgetID)
	return "", false
}

func (m *Manager) pluginAPI(ctx context.Context, p *Plugin) *hook.API {
	if p == nil {
		return hook.NewAPI().WithContext(ctx)
	}
	m.mu.RLock()
	script := m.scripts[p.ID]
	m.mu.RUnlock()
	if script != nil && script.api != nil {
		api := script.api.WithContext(ctx)
		m.log.DebugContext(ctx, "pluginAPI: found script api", "plugin", p.ID, "funcs", api.FuncNames())
		return api
	}
	slog.DebugContext(ctx, "pluginAPI: no script/api, returning empty API", "plugin", p.ID, "has_script", script != nil, "has_api", script != nil && script.api != nil)
	return hook.NewAPI().WithDomain(p.PluginDomain()).WithContext(ctx)
}

func pluginTemplateFuncs(ctx context.Context, options map[string]string, invoker hook.FuncInvoker) template.FuncMap {
	funcs := render.CommonFuncMap()
	maps.Copy(funcs, template.FuncMap{
		"plugin_option": func(key string) string { return options[key] },
		"hook_invoke": func(name string, args ...any) any {
			if invoker == nil {
				slog.DebugContext(ctx, "hookInvoke: api is nil", "name", name)
				return nil
			}
			result := invoker.InvokeFunc(ctx, name, script.ParseKVArgs(args))
			slog.DebugContext(ctx, "hookInvoke: called", "name", name, "result_nil", result == nil)
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
