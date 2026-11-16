package slotsdemo

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates storefront slot renderer registration for plugin authors.
type Plugin struct{}

// New returns the slots demo reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "slotsdemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("slotsdemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.slotsdemo.enabled") {
		return fmt.Errorf("slotsdemo plugin: disabled (plugins.slotsdemo.enabled=false)")
	}

	slots := h.Slots(p.Name())
	registrations := []struct {
		anchor    extapi.SlotAnchor
		placement extapi.Placement
		priority  int
		render    extapi.SlotRenderer
	}{
		{extapi.SlotLayoutFooter, extapi.PlacementAppend, 100, renderLayoutFooter},
		{extapi.SlotPDPInfo, extapi.PlacementAppend, 100, renderPDPInfo},
	}
	for _, reg := range registrations {
		if err := slots.RegisterRenderer(reg.anchor, reg.placement, reg.priority, reg.render); err != nil {
			return fmt.Errorf("slotsdemo plugin: register %s: %w", reg.anchor, err)
		}
	}
	return nil
}
