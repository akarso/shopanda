package cartdemo

import (
	"context"

	"github.com/akarso/shopanda/pkg/extapi"
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

func (s *HandlingFeeStep) Apply(_ context.Context, pctx extapi.PricingContext) error {
	if len(pctx.Items()) == 0 {
		return nil
	}
	amount := int64(DefaultHandlingFeeMinorUnits)
	if s.amountMinor != nil && *s.amountMinor > 0 {
		amount = *s.amountMinor
	}
	fee, err := extapi.NewMoney(amount, pctx.Currency())
	if err != nil {
		return err
	}
	return pctx.AddAdjustment(extapi.PricingAdjustment{
		Type:        extapi.AdjustmentFee,
		Code:        "cartdemo.handling_fee",
		Description: "Handling fee",
		Amount:      fee,
	})
}
