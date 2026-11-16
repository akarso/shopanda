package importdemo

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates integrator CSV import remap via import.product.row hook.
type Plugin struct{}

// New returns the import remap reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "importdemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("importdemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.importdemo.enabled") {
		return fmt.Errorf("importdemo plugin: disabled (plugins.importdemo.enabled=false)")
	}
	return pluginsdk.NewHost(h, p.Name()).Import().RegisterProductRow(100, func(ctx *extapi.ImportRowContext) error {
		RemapProductRow(ctx)
		return nil
	})
}
