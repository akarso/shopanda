package cartdemo

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

const (
	// DefaultHandlingFeeMinorUnits is the default flat fee when config is unset.
	DefaultHandlingFeeMinorUnits = 50
	// HandlingFeeStepName is the pricing pipeline step identifier.
	HandlingFeeStepName = "cartdemo.handling_fee"
)

// HandlingFeeStep adds a flat handling fee adjustment after promotions.
type HandlingFeeStep struct {
	amountMinor *int64
}

// NewHandlingFeeStep returns a pricing step that adds amountMinor units in the cart currency.
func NewHandlingFeeStep(amountMinor *int64) *HandlingFeeStep {
	return &HandlingFeeStep{amountMinor: amountMinor}
}

func (s *HandlingFeeStep) Name() string { return HandlingFeeStepName }

func (s *HandlingFeeStep) Apply(_ context.Context, pctx *pricing.PricingContext) error {
	if len(pctx.Items) == 0 {
		return nil
	}
	amount := int64(DefaultHandlingFeeMinorUnits)
	if s.amountMinor != nil && *s.amountMinor > 0 {
		amount = *s.amountMinor
	}
	fee, err := shared.NewMoney(amount, pctx.Currency)
	if err != nil {
		return err
	}
	adj, err := pricing.NewAdjustment(pricing.AdjustmentFee, "cartdemo.handling_fee", fee)
	if err != nil {
		return err
	}
	adj.Description = "Handling fee"
	pctx.Adjustments = append(pctx.Adjustments, adj)
	return nil
}
