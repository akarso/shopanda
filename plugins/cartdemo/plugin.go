package cartdemo

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates integrator cart rules: validate hook + positioned pricing step.
type Plugin struct{}

// New returns the cart rules reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "cartdemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("cartdemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.cartdemo.enabled") {
		return fmt.Errorf("cartdemo plugin: disabled (plugins.cartdemo.enabled=false)")
	}

	minQty := h.ConfigInt("plugins.cartdemo.min_quantity")
	if minQty <= 0 {
		minQty = DefaultMinQuantity
	}
	feeMinor := h.ConfigInt64("plugins.cartdemo.handling_fee_minor_units")
	if feeMinor <= 0 {
		feeMinor = DefaultHandlingFeeMinorUnits
	}

	if err := h.Hooks(p.Name()).Register(extapi.HookCartValidate, 100, minQuantityValidateHandler(minQty)); err != nil {
		return fmt.Errorf("cartdemo plugin: register cart.validate: %w", err)
	}
	pluginsdk.NewHost(h, p.Name()).Pricing().Register(NewHandlingFeeStep(&feeMinor), pluginsdk.After("promotions"))
	return nil
}
