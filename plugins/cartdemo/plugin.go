package cartdemo

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

// Plugin demonstrates integrator cart rules: validate hook + positioned pricing step.
type Plugin struct{}

// New returns the cart rules reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "cartdemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("cartdemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("cartdemo plugin: config not configured")
	}
	cfg := app.Config.Plugins.CartDemo
	if !cfg.Enabled {
		return fmt.Errorf("cartdemo plugin: disabled (plugins.cartdemo.enabled=false)")
	}

	minQty := cfg.MinQuantity
	if minQty <= 0 {
		minQty = DefaultMinQuantity
	}
	feeMinor := cfg.HandlingFeeMinorUnits
	if feeMinor <= 0 {
		feeMinor = DefaultHandlingFeeMinorUnits
	}

	if err := app.Hooks(p.Name()).Register(extapi.HookCartValidate, 100, minQuantityValidateHandler(minQty)); err != nil {
		return fmt.Errorf("cartdemo plugin: register cart.validate: %w", err)
	}
	app.RegisterPricingStep(NewHandlingFeeStep(&feeMinor), "after:promotions")
	return nil
}
