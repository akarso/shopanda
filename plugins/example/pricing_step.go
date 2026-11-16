package example

import (
	"context"

	"github.com/akarso/shopanda/pkg/extapi"
)

const defaultExampleFeeMinorUnits = 100

// ExampleFeeStep adds a fixed example fee adjustment to the pricing context.
type ExampleFeeStep struct {
	amountMinor *int64
}

// NewExampleFeeStep returns a pricing step that adds amountMinor units in the cart currency.
// When amountMinor points at live plugin state, admin config updates apply without restart.
func NewExampleFeeStep(amountMinor *int64) *ExampleFeeStep {
	return &ExampleFeeStep{amountMinor: amountMinor}
}

func (s *ExampleFeeStep) Name() string { return "example.fee" }

func (s *ExampleFeeStep) Apply(_ context.Context, pctx extapi.PricingContext) error {
	if len(pctx.Items()) == 0 {
		return nil
	}
	amount := int64(defaultExampleFeeMinorUnits)
	if s.amountMinor != nil && *s.amountMinor > 0 {
		amount = *s.amountMinor
	}
	fee, err := extapi.NewMoney(amount, pctx.Currency())
	if err != nil {
		return err
	}
	return pctx.AddAdjustment(extapi.PricingAdjustment{
		Type:        extapi.AdjustmentFee,
		Code:        "example.fee",
		Description: "Example plugin fee",
		Amount:      fee,
	})
}
