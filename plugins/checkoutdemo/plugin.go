package checkoutdemo

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates positioned checkout validation before order creation.
type Plugin struct{}

// New returns the checkout reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "checkoutdemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("checkoutdemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.checkoutdemo.enabled") {
		return fmt.Errorf("checkoutdemo plugin: disabled (plugins.checkoutdemo.enabled=false)")
	}
	pluginsdk.NewHost(h, p.Name()).Checkout().Register(&validateStep{}, pluginsdk.CheckoutBefore("create_order"))
	return nil
}

type validateStep struct{}

func (s *validateStep) Name() string { return "checkoutdemo_validate" }

func (s *validateStep) Execute(ctx context.Context, cctx extapi.CheckoutContext) error {
	if cctx == nil || !cctx.CartLoaded() {
		return fmt.Errorf("checkoutdemo: cart not loaded")
	}
	if cctx.CartTotalQuantity() <= 0 {
		return fmt.Errorf("checkoutdemo: cart must contain at least one item")
	}
	return nil
}

// Compile-time check.
var _ extapi.CheckoutStep = (*validateStep)(nil)
