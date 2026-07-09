package slotsdemo

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

// Plugin demonstrates storefront slot renderer registration for plugin authors.
type Plugin struct{}

// New returns the slots demo reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "slotsdemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("slotsdemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("slotsdemo plugin: config not configured")
	}
	if !app.Config.Plugins.SlotsDemo.Enabled {
		return fmt.Errorf("slotsdemo plugin: disabled (plugins.slotsdemo.enabled=false)")
	}

	slots := app.Slots(p.Name())
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
