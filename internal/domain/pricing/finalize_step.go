package pricing

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// FinalizeStep computes aggregate totals on a PricingContext.
type FinalizeStep struct{}

// NewFinalizeStep returns a new FinalizeStep.
func NewFinalizeStep() *FinalizeStep {
	return &FinalizeStep{}
}

func (s *FinalizeStep) Name() string { return "finalize" }

// Apply sums item totals into Subtotal, aggregates adjustments by type,
// and computes GrandTotal = Subtotal - DiscountsTotal + TaxTotal + FeesTotal.
// Accumulators are reset to zero so calling Apply twice is idempotent.
func (s *FinalizeStep) Apply(_ context.Context, pctx *PricingContext) error {
	zero := shared.MustZero(pctx.Currency)

	subtotal := zero
	for _, item := range pctx.Items {
		subtotal = subtotal.Add(item.Total)
	}
	pctx.Subtotal = subtotal

	discounts := zero
	taxes := zero
	fees := zero

	for _, item := range pctx.Items {
		for _, adj := range item.Adjustments {
			switch adj.Type {
			case AdjustmentDiscount:
				discounts = discounts.Add(adj.Amount)
			case AdjustmentTax:
				taxes = taxes.Add(adj.Amount)
			case AdjustmentFee:
				fees = fees.Add(adj.Amount)
			}
		}
	}

	for _, adj := range pctx.Adjustments {
		switch adj.Type {
		case AdjustmentDiscount:
			discounts = discounts.Add(adj.Amount)
		case AdjustmentTax:
			taxes = taxes.Add(adj.Amount)
		case AdjustmentFee:
			fees = fees.Add(adj.Amount)
		}
	}

	pctx.DiscountsTotal = discounts
	pctx.TaxTotal = taxes
	pctx.FeesTotal = fees

	pctx.GrandTotal = subtotal.Sub(discounts).Add(taxes).Add(fees)
	return nil
}
