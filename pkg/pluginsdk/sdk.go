package pluginsdk

import (
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// SDK wraps Host with typed registration helpers scoped to one plugin name.
type SDK struct {
	host   Host
	plugin string
}

// New returns an SDK for pluginName from a core *plugin.App (dual-mode).
// Prefer NewHost when Init already received a Host.
func New(app *plugin.App, pluginName string) *SDK {
	if app == nil {
		panic("pluginsdk: app must not be nil")
	}
	return NewHost(AsHostFor(app, pluginName), pluginName)
}

// NewHost returns an SDK for pluginName bound to Host.
func NewHost(h Host, pluginName string) *SDK {
	if h == nil {
		panic("pluginsdk: host must not be nil")
	}
	if pluginName == "" {
		panic("pluginsdk: plugin name must not be empty")
	}
	return &SDK{host: h, plugin: pluginName}
}

// Host returns the bound Host.
func (s *SDK) Host() Host { return s.host }

// PluginName returns the registrant name passed to New / NewHost.
func (s *SDK) PluginName() string {
	return s.plugin
}
