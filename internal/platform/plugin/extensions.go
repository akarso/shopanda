package plugin

import (
	"fmt"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

// Extensions exposes extension field registration to plugins during Init.
type Extensions struct {
	registry *extensionapp.Registry
}

// RegisterField registers an extension field definition.
// It forwards to the shared in-process registry used by core services.
func (e *Extensions) RegisterField(def domainext.FieldDef) error {
	if e == nil || e.registry == nil {
		return fmt.Errorf("plugin: extension registry not configured")
	}
	return e.registry.Register(def)
}

// SetExtensionRegistry wires the shared registry before plugin Init.
func (a *App) SetExtensionRegistry(registry *extensionapp.Registry) {
	if registry == nil {
		panic("plugin: extension registry must not be nil")
	}
	a.extensionRegistry = registry
}

// ExtensionRegistry returns the shared extension field registry.
func (a *App) ExtensionRegistry() *extensionapp.Registry {
	return a.extensionRegistry
}

// Extensions returns the plugin-facing extension registration API.
func (a *App) Extensions() *Extensions {
	if a.extensionRegistry == nil {
		a.extensionRegistry = extensionapp.NewRegistry()
	}
	return &Extensions{registry: a.extensionRegistry}
}
