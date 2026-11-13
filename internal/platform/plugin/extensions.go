package plugin

import (
	"context"
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
	a.extensionRegistryMu.Lock()
	defer a.extensionRegistryMu.Unlock()
	a.extensionRegistry = registry
}

// ExtensionRegistry returns the shared extension field registry.
func (a *App) ExtensionRegistry() *extensionapp.Registry {
	a.extensionRegistryMu.Lock()
	defer a.extensionRegistryMu.Unlock()
	return a.extensionRegistry
}

// Extensions returns the plugin-facing extension registration API.
func (a *App) Extensions() *Extensions {
	a.extensionRegistryMu.Lock()
	defer a.extensionRegistryMu.Unlock()
	if a.extensionRegistry == nil {
		a.extensionRegistry = extensionapp.NewRegistry()
	}
	return &Extensions{registry: a.extensionRegistry}
}

// WithPluginWrite returns ctx marked as a trusted write by pluginName for the
// given Access.PluginOnlyWrite field codes (only those). Pass the result to the
// service call, e.g. cartService.AddItem(ctx, ...); values for other plugin-only
// fields in the same call are still rejected. Panics on a blank name, no codes (also when called per request: ensure panics are recovered),
// or a code that is not a registered PluginOnlyWrite field (call it after RegisterField).
// Name only fields your plugin owns and never take codes from request input.
func (a *App) WithPluginWrite(ctx context.Context, pluginName string, fieldCodes ...string) context.Context {
	registry := a.ExtensionRegistry()
	for _, code := range fieldCodes {
		field, ok := registry.Get(code)
		if !ok {
			panic(fmt.Sprintf("plugin: WithPluginWrite: extension field %q is not registered (call RegisterField in Init before WithPluginWrite)", code))
		}
		if !field.Access.PluginOnlyWrite {
			panic(fmt.Sprintf("plugin: WithPluginWrite: extension field %q is not PluginOnlyWrite", code))
		}
	}
	return extensionapp.WithPluginWrite(ctx, pluginName, fieldCodes...)
}
