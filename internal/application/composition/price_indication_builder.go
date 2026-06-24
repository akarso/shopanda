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

// buildListingPriceIndicationsBatch prefetches variants, prices, and history for a PLP page.
func buildListingPriceIndicationsBatch(
	ctx context.Context,
	variants catalog.VariantRepository,
	prices pricing.PriceRepository,
	history pricing.PriceHistoryRepository,
	productIDs []string,
	storeID, currency string,
) (map[string]map[string]interface{}, error) {
	if variants == nil || prices == nil || history == nil || len(productIDs) == 0 {
		return nil, nil
	}

	byProduct, err := variants.ListByProductIDs(ctx, productIDs, maxVariantsForPriceIndication)
	if err != nil {
		return nil, fmt.Errorf("listing price indication: list variants: %w", err)
	}
	if len(byProduct) == 0 {
		return nil, nil
	}

	variantIDs := make([]string, 0)
	for _, productVariants := range byProduct {
		for _, variant := range productVariants {
			variantIDs = append(variantIDs, variant.ID)
		}
	}

	pricesByVariant, err := prices.FindByVariantsCurrencyAndStore(ctx, variantIDs, currency, storeID)
	if err != nil {
		return nil, fmt.Errorf("listing price indication: batch prices: %w", err)
	}

	since := time.Now().UTC().AddDate(0, 0, -30)
	lowestByVariant, err := history.LowestSinceByVariants(ctx, variantIDs, currency, storeID, since)
	if err != nil {
		return nil, fmt.Errorf("listing price indication: batch history: %w", err)
	}

	indications := make(map[string]map[string]interface{}, len(productIDs))
	for _, productID := range productIDs {
		productVariants := byProduct[productID]
		if len(productVariants) == 0 {
			continue
		}
		variantID, currentPrice := resolveLowestPriceVariantFromMaps(productVariants, pricesByVariant)
		if variantID == "" || currentPrice == nil {
			continue
		}
		blk, err := buildPriceIndicationBlockFromSnapshot(currentPrice, lowestByVariant[variantID], currency)
		if err != nil {
			return nil, err
		}
		if blk != nil {
			indications[productID] = blk.Data
		}
	}
	if len(indications) == 0 {
		return nil, nil
	}
	return indications, nil
}

func resolveLowestPriceVariantFromMaps(
	productVariants []catalog.Variant,
	pricesByVariant map[string]*pricing.Price,
) (string, *pricing.Price) {
	var (
		bestVariantID string
		bestPrice     *pricing.Price
	)
	for _, variant := range productVariants {
		price := pricesByVariant[variant.ID]
		if price == nil {
			continue
		}
		if bestPrice == nil || price.Amount.Amount() < bestPrice.Amount.Amount() {
			bestPrice = price
			bestVariantID = variant.ID
		}
	}
	return bestVariantID, bestPrice
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
	return buildPriceIndicationBlockFromSnapshot(currentPrice, lowest, currency)
}

func buildPriceIndicationBlockFromSnapshot(
	currentPrice *pricing.Price,
	lowest *pricing.PriceSnapshot,
	currency string,
) (*Block, error) {
	if currentPrice == nil {
		return nil, nil
	}
	if currency == "" {
		currency = "EUR"
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
