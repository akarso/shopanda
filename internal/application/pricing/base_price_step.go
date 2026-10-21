package pricing

import (
	"context"
	"fmt"

	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// BasePriceStep populates item prices via ResolveUnitPrice.
// The step name stays "base" so plugin positions that target it do not move.
type BasePriceStep struct {
	src    UnitPriceSources
	legacy bool
}

// NewBasePriceStep returns the production step. Prices, variants, products,
// and bundle components are all required. A nil catalog dependency panics so
// a dynamic bundle cannot fall through to its stored price row.
func NewBasePriceStep(src UnitPriceSources) *BasePriceStep {
	if src.Prices == nil || src.Variants == nil || src.Products == nil || src.Components == nil {
		panic("pricing: BasePriceStep requires prices, variants, products, and bundle components")
	}
	return &BasePriceStep{src: src}
}

// NewBasePriceStepLegacy prices every variant from its stored row and does not
// apply bundle or grouped rules. It exists for tests that are not exercising
// composition. Production wiring uses NewBasePriceStep.
func NewBasePriceStepLegacy(prices domain.PriceRepository) *BasePriceStep {
	if prices == nil {
		panic("pricing: price repository is required")
	}
	return &BasePriceStep{src: UnitPriceSources{Prices: prices}, legacy: true}
}

func (s *BasePriceStep) Name() string { return "base" }

// Apply looks up the base price for each item and sets UnitPrice and Total.
func (s *BasePriceStep) Apply(ctx context.Context, pctx *domain.PricingContext) error {
	storeID, _ := pctx.Meta["store_id"].(string)
	for i, item := range pctx.Items {
		var (
			amount shared.Money
			err    error
		)
		if s.legacy {
			amount, err = lookupStoredUnitPrice(ctx, s.src.Prices, item.VariantID, pctx.Currency, storeID)
		} else {
			amount, err = ResolveUnitPrice(ctx, s.src, item.VariantID, pctx.Currency, storeID)
		}
		if err != nil {
			return fmt.Errorf("base price: %w", err)
		}
		total, err := amount.MulChecked(int64(item.Quantity))
		if err != nil {
			return fmt.Errorf("base price: variant %s: %w", item.VariantID, err)
		}
		pctx.Items[i].UnitPrice = amount
		pctx.Items[i].Total = total
	}
	return nil
}
