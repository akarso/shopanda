package pluginsdk

import (
	"context"

	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/pkg/extapi"
)

// AdaptTaxCalculator wraps a public TaxCalculator as a core tax.Calculator
// for registration via Host.RegisterTaxCalculator.
func AdaptTaxCalculator(calc extapi.TaxCalculator) tax.Calculator {
	if calc == nil {
		panic("pluginsdk: AdaptTaxCalculator: calc must not be nil")
	}
	return &taxCalculatorBridge{calc: calc}
}

type taxCalculatorBridge struct {
	calc extapi.TaxCalculator
}

func (b *taxCalculatorBridge) Calculate(ctx context.Context, pctx *domainpricing.PricingContext) error {
	view := &pricingContextView{pctx: pctx}
	return b.calc.Calculate(ctx, view)
}
