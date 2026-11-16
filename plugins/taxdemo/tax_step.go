package taxdemo

import (
	"context"

	"github.com/akarso/shopanda/pkg/extapi"
)

// TaxStep delegates tax calculation to a registered extapi.TaxCalculator port.
type TaxStep struct {
	calc extapi.TaxCalculator
}

// NewTaxStep returns a pricing pipeline step that invokes calc.
func NewTaxStep(calc extapi.TaxCalculator) *TaxStep {
	return &TaxStep{calc: calc}
}

func (s *TaxStep) Name() string { return TaxStepName }

func (s *TaxStep) Apply(ctx context.Context, pctx extapi.PricingContext) error {
	if s == nil || s.calc == nil {
		return nil
	}
	return s.calc.Calculate(ctx, pctx)
}
