package exportdemo

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates integrator CSV export remap via export.product.row hook.
type Plugin struct{}

// New returns the export remap reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "exportdemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("exportdemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("exportdemo plugin: config not configured")
	}
	if !app.Config.Plugins.ExportDemo.Enabled {
		return fmt.Errorf("exportdemo plugin: disabled (plugins.exportdemo.enabled=false)")
	}
	return pluginsdk.New(app, p.Name()).Export().RegisterProductRow(100, func(ctx *extapi.ExportRowContext) error {
		RemapProductRow(ctx)
		return nil
	})
}
