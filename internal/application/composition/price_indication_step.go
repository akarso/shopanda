package composition

import (
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
)

// PriceIndicationStep adds a price_indication block to the PDP containing the
// lowest price in the last 30 days (EU Omnibus directive). The block is only
// added when a lower historical price exists.
type PriceIndicationStep struct {
	variants catalog.VariantRepository
	prices   pricing.PriceRepository
	history  pricing.PriceHistoryRepository
}

// NewPriceIndicationStep creates a PriceIndicationStep.
func NewPriceIndicationStep(
	variants catalog.VariantRepository,
	prices pricing.PriceRepository,
	history pricing.PriceHistoryRepository,
) *PriceIndicationStep {
	return &PriceIndicationStep{
		variants: variants,
		prices:   prices,
		history:  history,
	}
}

func (s *PriceIndicationStep) Name() string { return "price_indication" }

// Apply looks up the current price and the lowest price in the last 30 days
// for the first variant. If the current price is higher than the historical
// lowest, a price_indication block is appended.
func (s *PriceIndicationStep) Apply(ctx *ProductContext) error {
	if ctx == nil || ctx.Product == nil {
		return nil
	}

	variants, err := s.variants.ListByProductID(ctx.Ctx, ctx.Product.ID, 0, 1)
	if err != nil {
		return fmt.Errorf("price indication: list variants: %w", err)
	}
	if len(variants) == 0 {
		return nil
	}
	variantID := variants[0].ID

	currency := ctx.Currency
	if currency == "" {
		currency = "EUR"
	}

	currentPrice, err := s.lookupCurrentPrice(ctx, variantID, currency)
	if err != nil {
		return fmt.Errorf("price indication: current price: %w", err)
	}
	if currentPrice == nil {
		return nil
	}

	since := time.Now().UTC().AddDate(0, 0, -30)
	lowest, err := s.history.LowestSince(ctx.Ctx, variantID, currency, ctx.StoreID, since)
	if err != nil {
		return fmt.Errorf("price indication: lowest since: %w", err)
	}
	if lowest == nil {
		return nil
	}

	// Only attach the block when the lowest historical price differs from
	// the current price, indicating a price change has occurred.
	if lowest.Amount.Amount() >= currentPrice.Amount.Amount() {
		return nil
	}

	ctx.Blocks = append(ctx.Blocks, Block{
		Type: "price_indication",
		Data: map[string]interface{}{
			"current_price":    fmt.Sprintf("%.2f", float64(currentPrice.Amount.Amount())/100.0),
			"lowest_30d_price": fmt.Sprintf("%.2f", float64(lowest.Amount.Amount())/100.0),
			"currency":         currentPrice.Amount.Currency(),
			"recorded_at":      lowest.RecordedAt.Format(time.RFC3339),
		},
	})
	return nil
}

func (s *PriceIndicationStep) lookupCurrentPrice(ctx *ProductContext, variantID, currency string) (*pricing.Price, error) {
	price, err := s.prices.FindByVariantCurrencyAndStore(ctx.Ctx, variantID, currency, ctx.StoreID)
	if err != nil {
		return nil, fmt.Errorf("find price: %w", err)
	}
	if price == nil && ctx.StoreID != "" {
		price, err = s.prices.FindByVariantCurrencyAndStore(ctx.Ctx, variantID, currency, "")
		if err != nil {
			return nil, fmt.Errorf("find price (fallback): %w", err)
		}
	}
	return price, nil
}
