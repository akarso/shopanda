package pluginsdk

import (
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// SDK wraps plugin.App with typed registration helpers scoped to one plugin name.
type SDK struct {
	app    *plugin.App
	plugin string
}

// New returns an SDK for pluginName. Panics when app or pluginName is empty.
func New(app *plugin.App, pluginName string) *SDK {
	if app == nil {
		panic("pluginsdk: app must not be nil")
	}
	if pluginName == "" {
		panic("pluginsdk: plugin name must not be empty")
	}
	return &SDK{app: app, plugin: pluginName}
}

// App returns the underlying plugin.App.
func (s *SDK) App() *plugin.App {
	return s.app
}

// PluginName returns the registrant name passed to New.
func (s *SDK) PluginName() string {
	return s.plugin
}
