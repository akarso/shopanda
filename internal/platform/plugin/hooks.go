package plugin

import (
	"fmt"

	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
)

// Hooks exposes hook registration to plugins during Init.
type Hooks struct {
	registry   *hooksapp.Registry
	registrant string
}

// Register adds a handler for hook at priority (lower runs first).
func (h *Hooks) Register(hook string, priority int, handler hooksapp.Handler) error {
	if h == nil || h.registry == nil {
		return fmt.Errorf("plugin: hook registry not configured")
	}
	return h.registry.Register(hook, priority, h.registrant, handler)
}

// SetHookRegistry wires the shared hook registry before plugin Init.
func (a *App) SetHookRegistry(registry *hooksapp.Registry) {
	if registry == nil {
		panic("plugin: hook registry must not be nil")
	}
	a.hookRegistryMu.Lock()
	defer a.hookRegistryMu.Unlock()
	a.hookRegistry = registry
}

// HookRegistry returns the shared hook registry.
func (a *App) HookRegistry() *hooksapp.Registry {
	a.hookRegistryMu.Lock()
	defer a.hookRegistryMu.Unlock()
	return a.hookRegistry
}

// Hooks returns plugin-facing hook registration scoped to registrant.
func (a *App) Hooks(registrant string) *Hooks {
	a.hookRegistryMu.Lock()
	defer a.hookRegistryMu.Unlock()
	if a.hookRegistry == nil {
		a.hookRegistry = hooksapp.NewRegistry(a.Logger)
	}
	return &Hooks{registry: a.hookRegistry, registrant: registrant}
}
