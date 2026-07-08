package plugin

import (
	"fmt"

	slotsapp "github.com/akarso/shopanda/internal/application/slots"
	"github.com/akarso/shopanda/pkg/extapi"
)

// Slots exposes slot renderer registration to plugins during Init.
type Slots struct {
	registry   *slotsapp.Registry
	registrant string
}

// RegisterRenderer adds a renderer for anchor at placement (lower priority runs first).
func (s *Slots) RegisterRenderer(anchor extapi.SlotAnchor, placement extapi.Placement, priority int, renderer extapi.SlotRenderer) error {
	if s == nil || s.registry == nil {
		return fmt.Errorf("plugin: slot registry not configured")
	}
	if renderer == nil {
		return fmt.Errorf("plugin: slot renderer must not be nil")
	}
	p, err := slotsapp.ParsePlacement(string(placement))
	if err != nil {
		return err
	}
	return s.registry.RegisterRenderer(string(anchor), p, priority, s.registrant, func(ctx *slotsapp.RenderContext) string {
		return renderer(toExtAPISlotRenderContext(ctx))
	})
}

// SetSlotRegistry wires the shared slot registry before plugin Init.
func (a *App) SetSlotRegistry(registry *slotsapp.Registry) {
	if registry == nil {
		panic("plugin: slot registry must not be nil")
	}
	a.slotRegistryMu.Lock()
	defer a.slotRegistryMu.Unlock()
	a.slotRegistry = registry
}

// SlotRegistry returns the shared slot registry.
func (a *App) SlotRegistry() *slotsapp.Registry {
	a.slotRegistryMu.Lock()
	defer a.slotRegistryMu.Unlock()
	return a.slotRegistry
}

// Slots returns plugin-facing slot registration scoped to registrant.
func (a *App) Slots(registrant string) *Slots {
	a.slotRegistryMu.Lock()
	defer a.slotRegistryMu.Unlock()
	if a.slotRegistry == nil {
		a.slotRegistry = slotsapp.NewRegistry(a.Logger)
	}
	return &Slots{registry: a.slotRegistry, registrant: registrant}
}

func toExtAPISlotRenderContext(ctx *slotsapp.RenderContext) *extapi.SlotRenderContext {
	if ctx == nil {
		return &extapi.SlotRenderContext{}
	}
	return &extapi.SlotRenderContext{
		Anchor: ctx.Anchor,
		Data:   ctx.Data,
	}
}
