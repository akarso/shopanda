package pluginsdk

import (
	"github.com/akarso/shopanda/pkg/extapi"
)

// CheckoutPosition selects where a plugin checkout step runs in the core workflow.
type CheckoutPosition string

// CheckoutStart inserts the step before the first core checkout step.
func CheckoutStart() CheckoutPosition {
	return CheckoutPosition(extapi.CheckoutPositionStart)
}

// CheckoutEnd appends the step after the last core checkout step (legacy default).
func CheckoutEnd() CheckoutPosition {
	return CheckoutPosition(extapi.CheckoutPositionEnd)
}

// CheckoutAfter returns a position immediately after the named core step or alias.
func CheckoutAfter(step string) CheckoutPosition {
	return CheckoutPosition(extapi.AfterCheckoutStep(step))
}

// CheckoutBefore returns a position immediately before the named core step or alias.
func CheckoutBefore(step string) CheckoutPosition {
	return CheckoutPosition(extapi.BeforeCheckoutStep(step))
}

// Checkout registers positioned checkout workflow steps.
type Checkout struct {
	sdk *SDK
}

// Checkout returns checkout registration helpers for the SDK plugin.
func (s *SDK) Checkout() *Checkout {
	return &Checkout{sdk: s}
}

// Register adds step to the checkout workflow at position.
// Omit position to use the default (end — same as legacy append-only behavior).
func (c *Checkout) Register(step any, position ...CheckoutPosition) {
	if len(position) == 0 || position[0] == "" {
		c.sdk.app.RegisterCheckoutStep(step)
		return
	}
	c.sdk.app.RegisterCheckoutStep(step, string(position[0]))
}
