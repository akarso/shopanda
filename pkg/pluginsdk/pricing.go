package pluginsdk

import (
	"github.com/akarso/shopanda/pkg/extapi"
)

// PricingPosition selects where a plugin pricing step runs in the core pipeline.
type PricingPosition string

// After returns a position immediately after the named core step or alias.
func After(step string) PricingPosition {
	return PricingPosition(extapi.AfterPricingStep(step))
}

// Before returns a position immediately before the named core step or alias.
func Before(step string) PricingPosition {
	return PricingPosition(extapi.BeforePricingStep(step))
}

// Pricing registers positioned pricing pipeline steps.
type Pricing struct {
	sdk *SDK
}

// Pricing returns pricing registration helpers for the SDK plugin.
func (s *SDK) Pricing() *Pricing {
	return &Pricing{sdk: s}
}

// Register adds step to the pricing pipeline at position.
// Omit position to use the default (after base price).
func (p *Pricing) Register(step any, position ...PricingPosition) {
	if len(position) == 0 || position[0] == "" {
		p.sdk.app.RegisterPricingStep(step)
		return
	}
	p.sdk.app.RegisterPricingStep(step, string(position[0]))
}
