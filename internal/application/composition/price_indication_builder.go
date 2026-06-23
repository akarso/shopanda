package composition

import (
	"context"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
)

const metaPriceIndications = "price_indications"

const maxVariantsForPriceIndication = 100

// priceIndicationData holds Omnibus block payload fields.
type priceIndicationData struct {
	CurrentPrice   string
	Lowest30dPrice string
	Currency       string
	RecordedAt     string
}

func buildPriceIndicationBlock(
	ctx context.Context,
	variants catalog.VariantRepository,
	prices pricing.PriceRepository,
	history pricing.PriceHistoryRepository,
	productID, storeID, currency string,
) (*Block, error) {
	if variants == nil || prices == nil || history == nil {
		return nil, nil
	}
	productVariants, err := variants.ListByProductID(ctx, productID, 0, 1)
	if err != nil {
		return nil, fmt.Errorf("price indication: list variants: %w", err)
	}
	if len(productVariants) == 0 {
		return nil, nil
	}
	currentPrice, err := lookupCurrentPrice(ctx, prices, productVariants[0].ID, currency, storeID)
	if err != nil {
		return nil, fmt.Errorf("price indication: current price: %w", err)
	}
	return buildPriceIndicationBlockFromVariant(ctx, history, productVariants[0].ID, storeID, currency, currentPrice)
}

// buildListingPriceIndicationBlock uses the lowest store-scoped variant price,
// matching the PLP search engine's displayed price selection.
func buildListingPriceIndicationBlock(
	ctx context.Context,
	variants catalog.VariantRepository,
	prices pricing.PriceRepository,
	history pricing.PriceHistoryRepository,
	productID, storeID, currency string,
) (*Block, error) {
	if variants == nil || prices == nil || history == nil {
		return nil, nil
	}
	variantID, currentPrice, err := resolveLowestPriceVariant(ctx, variants, prices, productID, storeID, currency)
	if err != nil {
		return nil, err
	}
	if variantID == "" {
		return nil, nil
	}
	return buildPriceIndicationBlockFromVariant(ctx, history, variantID, storeID, currency, currentPrice)
}

func resolveLowestPriceVariant(
	ctx context.Context,
	variants catalog.VariantRepository,
	prices pricing.PriceRepository,
	productID, storeID, currency string,
) (string, *pricing.Price, error) {
	productVariants, err := variants.ListByProductID(ctx, productID, 0, maxVariantsForPriceIndication)
	if err != nil {
		return "", nil, fmt.Errorf("price indication: list variants: %w", err)
	}
	var (
		bestVariantID string
		bestPrice     *pricing.Price
	)
	for _, variant := range productVariants {
		price, err := lookupCurrentPrice(ctx, prices, variant.ID, currency, storeID)
		if err != nil {
			return "", nil, fmt.Errorf("price indication: current price: %w", err)
		}
		if price == nil {
			continue
		}
		if bestPrice == nil || price.Amount.Amount() < bestPrice.Amount.Amount() {
			bestPrice = price
			bestVariantID = variant.ID
		}
	}
	return bestVariantID, bestPrice, nil
}

func buildPriceIndicationBlockFromVariant(
	ctx context.Context,
	history pricing.PriceHistoryRepository,
	variantID, storeID, currency string,
	currentPrice *pricing.Price,
) (*Block, error) {
	if currentPrice == nil {
		return nil, nil
	}
	if currency == "" {
		currency = "EUR"
	}

	since := time.Now().UTC().AddDate(0, 0, -30)
	lowest, err := history.LowestSince(ctx, variantID, currency, storeID, since)
	if err != nil {
		return nil, fmt.Errorf("price indication: lowest since: %w", err)
	}
	if lowest == nil {
		return nil, nil
	}
	if lowest.Amount.Amount() >= currentPrice.Amount.Amount() {
		return nil, nil
	}

	data := priceIndicationData{
		CurrentPrice:   formatMajorUnits(currentPrice.Amount.Amount()),
		Lowest30dPrice: formatMajorUnits(lowest.Amount.Amount()),
		Currency:       currentPrice.Amount.Currency(),
		RecordedAt:     lowest.RecordedAt.Format(time.RFC3339),
	}
	return &Block{
		Type: "price_indication",
		Data: map[string]interface{}{
			"current_price":    data.CurrentPrice,
			"lowest_30d_price": data.Lowest30dPrice,
			"currency":         data.Currency,
			"recorded_at":      data.RecordedAt,
		},
	}, nil
}

func lookupCurrentPrice(ctx context.Context, prices pricing.PriceRepository, variantID, currency, storeID string) (*pricing.Price, error) {
	price, err := prices.FindByVariantCurrencyAndStore(ctx, variantID, currency, storeID)
	if err != nil {
		return nil, fmt.Errorf("find price: %w", err)
	}
	if price == nil && storeID != "" {
		price, err = prices.FindByVariantCurrencyAndStore(ctx, variantID, currency, "")
		if err != nil {
			return nil, fmt.Errorf("find price (fallback): %w", err)
		}
	}
	return price, nil
}

func formatMajorUnits(minor int64) string {
	return fmt.Sprintf("%.2f", float64(minor)/100.0)
}

// PriceIndicationsFromMeta reads per-product Omnibus data populated by ListingPriceIndicationStep.
func PriceIndicationsFromMeta(meta map[string]interface{}) map[string]map[string]interface{} {
	if meta == nil {
		return nil
	}
	raw, ok := meta[metaPriceIndications]
	if !ok || raw == nil {
		return nil
	}
	typed, ok := raw.(map[string]map[string]interface{})
	if !ok {
		return nil
	}
	return typed
}
