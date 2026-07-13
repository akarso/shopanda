package pricing

import (
	"context"

	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/tax"
)

// TaxStep delegates tax calculation to a registered tax.Calculator port.
type TaxStep struct {
	calc tax.Calculator
}

// NewTaxStep returns a pricing pipeline step that invokes calc.
func NewTaxStep(calc tax.Calculator) *TaxStep {
	return &TaxStep{calc: calc}
}

func (s *TaxStep) Name() string { return "tax" }

// Apply runs the configured tax calculator.
func (s *TaxStep) Apply(ctx context.Context, pctx *domain.PricingContext) error {
	if s == nil || s.calc == nil {
		return nil
	}
	return s.calc.Calculate(ctx, pctx)
}
