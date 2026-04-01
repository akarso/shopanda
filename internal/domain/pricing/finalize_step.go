package pricing

// FinalizeStep computes aggregate totals on a PricingContext.
type FinalizeStep struct{}

// NewFinalizeStep returns a new FinalizeStep.
func NewFinalizeStep() *FinalizeStep {
	return &FinalizeStep{}
}

func (s *FinalizeStep) Name() string { return "finalize" }

// Apply sums item totals into Subtotal, aggregates adjustments by type,
// and computes GrandTotal = Subtotal - DiscountsTotal + TaxTotal + FeesTotal.
func (s *FinalizeStep) Apply(ctx *PricingContext) error {
	subtotal := ctx.Subtotal
	for _, item := range ctx.Items {
		subtotal = subtotal.Add(item.Total)
	}
	ctx.Subtotal = subtotal

	discounts := ctx.DiscountsTotal
	taxes := ctx.TaxTotal
	fees := ctx.FeesTotal

	for _, item := range ctx.Items {
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

	for _, adj := range ctx.Adjustments {
		switch adj.Type {
		case AdjustmentDiscount:
			discounts = discounts.Add(adj.Amount)
		case AdjustmentTax:
			taxes = taxes.Add(adj.Amount)
		case AdjustmentFee:
			fees = fees.Add(adj.Amount)
		}
	}

	ctx.DiscountsTotal = discounts
	ctx.TaxTotal = taxes
	ctx.FeesTotal = fees

	ctx.GrandTotal = subtotal.Sub(discounts).Add(taxes).Add(fees)
	return nil
}
