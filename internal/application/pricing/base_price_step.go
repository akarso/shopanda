package pricing

import (
	"context"
	"fmt"

	domain "github.com/akarso/shopanda/internal/domain/pricing"
)

// BasePriceStep populates item prices from the price repository.
type BasePriceStep struct {
	prices domain.PriceRepository
}

// NewBasePriceStep returns a new BasePriceStep.
func NewBasePriceStep(prices domain.PriceRepository) *BasePriceStep {
	return &BasePriceStep{prices: prices}
}

func (s *BasePriceStep) Name() string { return "base" }

// Apply looks up the base price for each item and sets UnitPrice and Total.
func (s *BasePriceStep) Apply(ctx context.Context, pctx *domain.PricingContext) error {
	storeID, _ := pctx.Meta["store_id"].(string) // empty = global/default

	for i, item := range pctx.Items {
		price, err := s.prices.FindByVariantCurrencyAndStore(ctx, item.VariantID, pctx.Currency, storeID)
		if err != nil {
			return fmt.Errorf("base price: variant %s: %w", item.VariantID, err)
		}
		// Fall back to global price when store-scoped price not found.
		if price == nil && storeID != "" {
			price, err = s.prices.FindByVariantCurrencyAndStore(ctx, item.VariantID, pctx.Currency, "")
			if err != nil {
				return fmt.Errorf("base price: variant %s (fallback): %w", item.VariantID, err)
			}
		}
		if price == nil {
			return fmt.Errorf("base price: no price for variant %s in %s", item.VariantID, pctx.Currency)
		}
		total, err := price.Amount.MulChecked(int64(item.Quantity))
		if err != nil {
			return fmt.Errorf("base price: variant %s: %w", item.VariantID, err)
		}
		pctx.Items[i].UnitPrice = price.Amount
		pctx.Items[i].Total = total
	}
	return nil
}
