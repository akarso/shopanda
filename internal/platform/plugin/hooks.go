package plugin

import (
	"fmt"

	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
	"github.com/akarso/shopanda/pkg/extapi"
)

// Hooks exposes hook registration to plugins during Init.
type Hooks struct {
	registry   *hooksapp.Registry
	registrant string
}

// Register adds a handler for hook at priority (lower runs first).
func (h *Hooks) Register(hook extapi.HookPoint, priority int, handler extapi.HookHandler) error {
	if h == nil || h.registry == nil {
		return fmt.Errorf("plugin: hook registry not configured")
	}
	if handler == nil {
		return fmt.Errorf("plugin: hook handler must not be nil")
	}
	return h.registry.Register(string(hook), priority, h.registrant, func(ctx *hooksapp.Context) error {
		extCtx := toExtAPIHookContext(ctx)
		if err := handler(extCtx); err != nil {
			return err
		}
		syncHookPayload(ctx, extCtx)
		return nil
	})
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

func syncHookPayload(dst *hooksapp.Context, src *extapi.HookContext) {
	if dst == nil || src == nil || len(src.Payload) == 0 {
		return
	}
	for k, v := range src.Payload {
		dst.Set(k, v)
	}
}

func toExtAPIHookContext(ctx *hooksapp.Context) *extapi.HookContext {
	if ctx == nil {
		return &extapi.HookContext{}
	}
	out := &extapi.HookContext{Name: ctx.Name}
	if len(ctx.Payload) > 0 {
		out.Payload = make(map[string]interface{}, len(ctx.Payload))
		for k, v := range ctx.Payload {
			out.Payload[k] = v
		}
	}
	return out
}
