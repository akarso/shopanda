package taxdemo

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Compile-time check that the demo calculator satisfies the tax port.
var _ extapi.TaxCalculator = (*FlatRateCalculator)(nil)

// Plugin demonstrates integrator tax port replacement and replace-by-name pricing step.
type Plugin struct{}

// New returns the tax replacement reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "taxdemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("taxdemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.taxdemo.enabled") {
		return fmt.Errorf("taxdemo plugin: disabled (plugins.taxdemo.enabled=false)")
	}

	rateBPS := h.ConfigInt("plugins.taxdemo.flat_rate_bps")
	if rateBPS <= 0 {
		rateBPS = DefaultFlatRateBPS
	}

	calc := NewFlatRateCalculator(rateBPS)
	h.RegisterTaxCalculator(calc)
	pluginsdk.NewHost(h, p.Name()).Pricing().Register(NewTaxStep(calc), pluginsdk.Replace("tax"))
	return nil
}
