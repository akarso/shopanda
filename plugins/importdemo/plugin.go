package importdemo

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates integrator CSV import remap via import.product.row hook.
type Plugin struct{}

// New returns the import remap reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "importdemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("importdemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("importdemo plugin: config not configured")
	}
	if !app.Config.Plugins.ImportDemo.Enabled {
		return fmt.Errorf("importdemo plugin: disabled (plugins.importdemo.enabled=false)")
	}
	return pluginsdk.New(app, p.Name()).Import().RegisterProductRow(100, func(ctx *extapi.ImportRowContext) error {
		RemapProductRow(ctx)
		return nil
	})
}
