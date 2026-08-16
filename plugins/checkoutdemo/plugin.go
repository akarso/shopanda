package checkoutdemo

import (
	"context"
	"fmt"

	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates positioned checkout validation before order creation.
type Plugin struct{}

// New returns the checkout reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "checkoutdemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("checkoutdemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("checkoutdemo plugin: config not configured")
	}
	if !app.Config.Plugins.CheckoutDemo.Enabled {
		return fmt.Errorf("checkoutdemo plugin: disabled (plugins.checkoutdemo.enabled=false)")
	}
	pluginsdk.New(app, p.Name()).Checkout().Register(&validateStep{}, pluginsdk.CheckoutBefore("create_order"))
	return nil
}

type validateStep struct{}

func (s *validateStep) Name() string { return "checkoutdemo_validate" }

func (s *validateStep) Execute(ctx context.Context, cctx *checkoutApp.Context) error {
	if cctx == nil || cctx.Cart == nil {
		return fmt.Errorf("checkoutdemo: cart not loaded")
	}
	if cctx.Cart.TotalQuantity() <= 0 {
		return fmt.Errorf("checkoutdemo: cart must contain at least one item")
	}
	return nil
}

// Compile-time check.
var _ checkoutApp.Step = (*validateStep)(nil)
