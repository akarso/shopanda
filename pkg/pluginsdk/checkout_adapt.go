package pluginsdk

import (
	"context"
	"fmt"

	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/pkg/extapi"
)

// AdaptCheckoutStep wraps a public CheckoutStep as a core checkout.Step.
func AdaptCheckoutStep(step extapi.CheckoutStep) checkoutApp.Step {
	if step == nil {
		panic("pluginsdk: AdaptCheckoutStep: step must not be nil")
	}
	return &checkoutStepBridge{step: step}
}

type checkoutStepBridge struct {
	step extapi.CheckoutStep
}

func (b *checkoutStepBridge) Name() string { return b.step.Name() }

func (b *checkoutStepBridge) Execute(ctx context.Context, cctx *checkoutApp.Context) error {
	return b.step.Execute(ctx, &checkoutContextView{cctx: cctx})
}

type checkoutContextView struct {
	cctx *checkoutApp.Context
}

func (v *checkoutContextView) CartID() string     { return v.cctx.CartID }
func (v *checkoutContextView) CustomerID() string { return v.cctx.CustomerID }
func (v *checkoutContextView) Currency() string   { return v.cctx.Currency }
func (v *checkoutContextView) CartLoaded() bool   { return v.cctx != nil && v.cctx.Cart != nil }

func (v *checkoutContextView) CartTotalQuantity() int {
	if v.cctx == nil || v.cctx.Cart == nil {
		return 0
	}
	return v.cctx.Cart.TotalQuantity()
}

func (v *checkoutContextView) CartItemCount() int {
	if v.cctx == nil || v.cctx.Cart == nil {
		return 0
	}
	return len(v.cctx.Cart.Items)
}

// Address always returns the zero value. Every checkout step registered on
// the workflow (any plugin, for any purpose) used to receive the full
// shipping/billing address on every step's payload, regardless of whether
// that step needed PII at all — a plugin that only wants to react to
// ShippingMethod/PaymentMethod/OrderID had no way to avoid seeing a
// customer's street address and name. Until there is a scoped,
// trust-gated way to grant PII to the specific step(s) that need it (same
// idea as the cart plugin-only-field owner check), the public SDK does not
// expose it at all.
func (v *checkoutContextView) Address() extapi.Address {
	return extapi.Address{}
}

// ContactEmail always returns "". See Address.
func (v *checkoutContextView) ContactEmail() string {
	return ""
}

func (v *checkoutContextView) ShippingMethod() string {
	if v.cctx == nil {
		return ""
	}
	return v.cctx.Input.ShippingMethod
}

func (v *checkoutContextView) PaymentMethod() string {
	if v.cctx == nil {
		return ""
	}
	return v.cctx.Input.PaymentMethod
}

func (v *checkoutContextView) OrderID() string {
	if v.cctx == nil || v.cctx.Order == nil {
		return ""
	}
	return v.cctx.Order.ID
}

func (v *checkoutContextView) MetaGet(key string) (interface{}, bool) {
	return v.cctx.GetMeta(key)
}

func (v *checkoutContextView) MetaSet(key string, value interface{}) {
	v.cctx.SetMeta(key, value)
}

func coerceCheckoutStep(step any) (checkoutApp.Step, error) {
	if step == nil {
		return nil, fmt.Errorf("pluginsdk: checkout step must not be nil")
	}
	if s, ok := step.(extapi.CheckoutStep); ok {
		return AdaptCheckoutStep(s), nil
	}
	if s, ok := step.(checkoutApp.Step); ok {
		return s, nil
	}
	return nil, fmt.Errorf("pluginsdk: expected extapi.CheckoutStep or checkout.Step, got %T", step)
}
