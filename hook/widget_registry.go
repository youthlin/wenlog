package hook

import "sync"

// WidgetRegistry 管理所有已注册的组件 renderer，按 source:id 或 plugin:pluginID:id 索引。
// theme 和 plugin 包各自通过 Register 方法将组件注册进来，
// 模板渲染阶段通过 Get 方法查找 renderer。
type WidgetRegistry struct {
	mu      sync.RWMutex
	widgets map[string]WidgetRenderer
}

// NewWidgetRegistry 创建组件注册表。
func NewWidgetRegistry() *WidgetRegistry {
	return &WidgetRegistry{widgets: make(map[string]WidgetRenderer)}
}

// Register 注册一个组件 renderer。
func (r *WidgetRegistry) Register(w WidgetRenderer) {
	if r == nil || w == nil {
		return
	}
	decl := w.Meta()
	key := widgetDeclKey(decl.Source, decl.ID, decl.PluginID)
	r.mu.Lock()
	r.widgets[key] = w
	r.mu.Unlock()
}

// Get 按来源和 ID 查找组件 renderer。
// source 为 "builtin" / "theme" / "plugin"，pluginID 仅在 source="plugin" 时有值。
func (r *WidgetRegistry) Get(source, id, pluginID string) WidgetRenderer {
	if r == nil {
		return nil
	}
	key := widgetDeclKey(source, id, pluginID)
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.widgets[key]
}

func widgetDeclKey(source, id, pluginID string) string {
	if source == "plugin" && pluginID != "" {
		return "plugin:" + pluginID + ":" + id
	}
	return source + ":" + id
}
