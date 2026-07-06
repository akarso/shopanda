package plugin

import (
	"fmt"

	slotsapp "github.com/akarso/shopanda/internal/application/slots"
)

// Slots exposes slot renderer registration to plugins during Init.
type Slots struct {
	registry   *slotsapp.Registry
	registrant string
}

// RegisterRenderer adds a renderer for anchor at placement (lower priority runs first).
func (s *Slots) RegisterRenderer(anchor string, placement slotsapp.Placement, priority int, renderer slotsapp.Renderer) error {
	if s == nil || s.registry == nil {
		return fmt.Errorf("plugin: slot registry not configured")
	}
	return s.registry.RegisterRenderer(anchor, placement, priority, s.registrant, renderer)
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
