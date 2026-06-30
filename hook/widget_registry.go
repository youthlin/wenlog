package hook

import "sync"

// WidgetRegistry 管理所有已注册的组件实现，按 source:id 或 plugin:pluginID:id 索引。
// theme 和 plugin 包各自通过 Register 方法将组件注册进来，
// renderer 通过 Get 方法查找组件实现用于渲染。
type WidgetRegistry struct {
	mu      sync.RWMutex
	widgets map[string]Widget
}

// NewWidgetRegistry 创建组件注册表。
func NewWidgetRegistry() *WidgetRegistry {
	return &WidgetRegistry{widgets: make(map[string]Widget)}
}

// Register 注册一个组件实现。
func (r *WidgetRegistry) Register(w Widget) {
	if r == nil || w == nil {
		return
	}
	decl := w.Meta()
	key := widgetDeclKey(decl.Source, decl.ID, decl.PluginID)
	r.mu.Lock()
	r.widgets[key] = w
	r.mu.Unlock()
}

// Get 按来源和 ID 查找组件实现。
// source 为 "builtin" / "theme" / "plugin"，pluginID 仅在 source="plugin" 时有值。
func (r *WidgetRegistry) Get(source, id, pluginID string) Widget {
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
