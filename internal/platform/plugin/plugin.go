package plugin

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Plugin defines the contract for extending the system.
// Plugins register behavior during Init and are then invoked by the system.
type Plugin interface {
	// Name returns a unique identifier for the plugin.
	Name() string

	// Init initializes the plugin. Called once at startup.
	// The plugin should use the provided App to register event handlers,
	// pipeline steps, or other extensions.
	// Returning an error disables the plugin without crashing the system.
	Init(app *App) error
}

// App provides the system facilities available to plugins during initialization.
type App struct {
	Logger logger.Logger
	Bus    *event.Bus
	Config *config.Config
}
