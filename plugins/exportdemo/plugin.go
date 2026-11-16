package exportdemo

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates integrator CSV export remap via export.product.row hook.
type Plugin struct{}

// New returns the export remap reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "exportdemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("exportdemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.exportdemo.enabled") {
		return fmt.Errorf("exportdemo plugin: disabled (plugins.exportdemo.enabled=false)")
	}
	return pluginsdk.NewHost(h, p.Name()).Export().RegisterProductRow(100, func(ctx *extapi.ExportRowContext) error {
		RemapProductRow(ctx)
		return nil
	})
}
