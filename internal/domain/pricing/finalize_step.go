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
		sum, err := subtotal.Add(item.Total)
		if err != nil {
			return err
		}
		subtotal = sum
	}
	pctx.Subtotal = subtotal

	discounts := zero
	taxes := zero
	fees := zero

	for _, item := range pctx.Items {
		var err error
		for _, adj := range item.Adjustments {
			switch adj.Type {
			case AdjustmentDiscount:
				discounts, err = discounts.Add(adj.Amount)
			case AdjustmentTax:
				taxes, err = taxes.Add(adj.Amount)
			case AdjustmentFee:
				fees, err = fees.Add(adj.Amount)
			}
			if err != nil {
				return err
			}
		}
	}

	for _, adj := range pctx.Adjustments {
		var err error
		switch adj.Type {
		case AdjustmentDiscount:
			discounts, err = discounts.Add(adj.Amount)
		case AdjustmentTax:
			taxes, err = taxes.Add(adj.Amount)
		case AdjustmentFee:
			fees, err = fees.Add(adj.Amount)
		}
		if err != nil {
			return err
		}
	}

	pctx.DiscountsTotal = discounts
	pctx.TaxTotal = taxes
	pctx.FeesTotal = fees

	grand, err := subtotal.Sub(discounts)
	if err != nil {
		return err
	}
	grand, err = grand.Add(taxes)
	if err != nil {
		return err
	}
	grand, err = grand.Add(fees)
	if err != nil {
		return err
	}
	pctx.GrandTotal = grand
	return nil
}
