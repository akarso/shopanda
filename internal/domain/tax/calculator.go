package tax

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/pricing"
)

// Calculator applies tax to a pricing context during the pricing pipeline.
// Implementations mutate item adjustments and totals in place.
type Calculator interface {
	Calculate(ctx context.Context, pctx *pricing.PricingContext) error
}
