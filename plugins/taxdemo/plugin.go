package taxdemo

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates integrator tax port replacement and replace-by-name pricing step.
type Plugin struct{}

// New returns the tax replacement reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "taxdemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("taxdemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("taxdemo plugin: config not configured")
	}
	cfg := app.Config.Plugins.TaxDemo
	if !cfg.Enabled {
		return fmt.Errorf("taxdemo plugin: disabled (plugins.taxdemo.enabled=false)")
	}

	rateBPS := cfg.FlatRateBPS
	if rateBPS <= 0 {
		rateBPS = DefaultFlatRateBPS
	}

	calc := NewFlatRateCalculator(rateBPS)
	app.RegisterTaxCalculator(calc)
	pluginsdk.New(app, p.Name()).Pricing().Register(NewTaxStep(calc), pluginsdk.Replace("tax"))
	return nil
}
