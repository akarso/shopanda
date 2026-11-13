package plugin

import (
	"context"
	"fmt"
	"strings"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

// Extensions exposes extension field registration to plugins during Init.
type Extensions struct {
	registry *extensionapp.Registry
	owner    string // empty = unowned (legacy app.Extensions())
}

// RegisterField registers an extension field definition.
// It forwards to the shared in-process registry used by core services.
func (e *Extensions) RegisterField(def domainext.FieldDef) error {
	if e == nil || e.registry == nil {
		return fmt.Errorf("plugin: extension registry not configured")
	}
	if e.owner != "" {
		return e.registry.RegisterOwned(def, e.owner)
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
// Fields registered during a plugin's Init are assigned to that plugin by the
// loader after Init returns (and rolled back if Init fails); fields registered
// at any other time are unowned.
func (a *App) Extensions() *Extensions {
	a.extensionRegistryMu.Lock()
	defer a.extensionRegistryMu.Unlock()
	if a.extensionRegistry == nil {
		a.extensionRegistry = extensionapp.NewRegistry()
	}
	return &Extensions{registry: a.extensionRegistry}
}

// ExtensionsFor is like Extensions but records owner (the plugin name) explicitly,
// for code running outside plugin Init. Panics on a blank owner. During a plugin's
// Init the loader verifies afterwards that every field was registered under that
// plugin's own name and fails the Init (rolling the fields back) otherwise; outside
// Init the name is not verified.
func (a *App) ExtensionsFor(owner string) *Extensions {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		panic("plugin: extension owner must not be empty")
	}
	e := a.Extensions()
	e.owner = owner
	return e
}

// UnownedPluginOnlyFields lists plugin-only extension fields with no recorded owner;
// any trusted registrant may write them.
func (a *App) UnownedPluginOnlyFields() []string {
	return a.ExtensionRegistry().UnownedPluginOnly()
}

// WarnUnownedPluginOnlyFields logs plugin-only fields that have no owner. Call it
// after the last registration phase (plugin Init and persisted-field load).
func (a *App) WarnUnownedPluginOnlyFields() {
	unowned := a.UnownedPluginOnlyFields()
	if len(unowned) == 0 || a.Logger == nil {
		return
	}
	a.Logger.Warn("extension.plugin_only.unowned", map[string]interface{}{
		"fields": unowned,
		"hint":   "register plugin-only fields during plugin Init (or via App.ExtensionsFor) so the owner is recorded",
	})
}

// extensionCodeSnapshot returns the currently registered extension field codes.
func (a *App) extensionCodeSnapshot() map[string]struct{} {
	registry := a.ExtensionRegistry()
	out := make(map[string]struct{})
	for _, code := range registry.Codes() {
		out[code] = struct{}{}
	}
	return out
}

// settleExtensionFields runs after a plugin's Init. Fields registered since
// before: on Init failure they are removed; otherwise unowned ones are assigned to
// name, and a field owned by another plugin fails the Init and is rolled back.
func (a *App) settleExtensionFields(name string, before map[string]struct{}, initErr error) error {
	registry := a.ExtensionRegistry()
	var added []string
	for _, code := range registry.Codes() {
		if _, existed := before[code]; !existed {
			added = append(added, code)
		}
	}
	rollback := func() {
		for _, code := range added {
			registry.Remove(code)
		}
	}
	if initErr != nil {
		rollback()
		return initErr
	}
	for _, code := range added {
		if registry.AssignOwner(code, name) {
			continue
		}
		if owner := registry.Owner(code); owner != name {
			rollback()
			return fmt.Errorf("plugin %q registered extension field %q under owner %q", name, code, owner)
		}
	}
	return nil
}

// PluginWriteContext returns ctx marked as a trusted write by pluginName for the
// given Access.PluginOnlyWrite field codes (only those). It returns an error for a
// blank name, no codes, an unregistered or non-plugin-only code, or a field owned by
// another plugin. Prefer it in events, workers and goroutines, where a panic is not
// recovered. Call it after RegisterField. The name is caller-supplied, so this is a
// guard against mistakes, not a security boundary.
func (a *App) PluginWriteContext(ctx context.Context, pluginName string, fieldCodes ...string) (context.Context, error) {
	pluginName = strings.TrimSpace(pluginName)
	if pluginName == "" {
		return ctx, fmt.Errorf("plugin: WithPluginWrite: plugin name must not be empty")
	}
	if len(fieldCodes) == 0 {
		return ctx, fmt.Errorf("plugin: WithPluginWrite: at least one field code is required")
	}
	registry := a.ExtensionRegistry()
	for _, code := range fieldCodes {
		field, ok := registry.Get(code)
		if !ok {
			return ctx, fmt.Errorf("plugin: WithPluginWrite: extension field %q is not registered (call RegisterField in Init before WithPluginWrite)", code)
		}
		if !field.Access.PluginOnlyWrite {
			return ctx, fmt.Errorf("plugin: WithPluginWrite: extension field %q is not PluginOnlyWrite", code)
		}
		if owner := registry.Owner(code); owner != "" && owner != pluginName {
			return ctx, fmt.Errorf("plugin: WithPluginWrite: extension field %q is owned by %q, not %q", code, owner, pluginName)
		}
	}
	return extensionapp.WithPluginWrite(ctx, pluginName, fieldCodes...), nil
}

// WithPluginWrite is PluginWriteContext that panics on error. A panic is recovered
// only by HTTP middleware and plugin Init; in events, workers and goroutines use
// PluginWriteContext instead. Never build the codes from runtime/request input.
func (a *App) WithPluginWrite(ctx context.Context, pluginName string, fieldCodes ...string) context.Context {
	out, err := a.PluginWriteContext(ctx, pluginName, fieldCodes...)
	if err != nil {
		panic(err.Error())
	}
	return out
}
