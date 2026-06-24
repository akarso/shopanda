package testutil

import (
	"context"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
)

// ListByProductIDsFromList loops ListByProductID for test mocks.
func ListByProductIDsFromList(
	ctx context.Context,
	listFn func(context.Context, string, int, int) ([]catalog.Variant, error),
	productIDs []string,
	limitPerProduct int,
) (map[string][]catalog.Variant, error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	out := make(map[string][]catalog.Variant, len(productIDs))
	for _, productID := range productIDs {
		variants, err := listFn(ctx, productID, 0, limitPerProduct)
		if err != nil {
			return nil, err
		}
		if len(variants) > 0 {
			out[productID] = variants
		}
	}
	return out, nil
}

// FindByVariantsCurrencyAndStoreFromFind loops FindByVariantCurrencyAndStore for test mocks.
func FindByVariantsCurrencyAndStoreFromFind(
	ctx context.Context,
	findFn func(context.Context, string, string, string) (*pricing.Price, error),
	variantIDs []string,
	currency, storeID string,
) (map[string]*pricing.Price, error) {
	if len(variantIDs) == 0 {
		return nil, nil
	}
	out := make(map[string]*pricing.Price, len(variantIDs))
	for _, variantID := range variantIDs {
		price, err := findFn(ctx, variantID, currency, storeID)
		if err != nil {
			return nil, err
		}
		if price != nil {
			out[variantID] = price
		}
	}
	return out, nil
}

// LowestSinceByVariantsFromLowest loops LowestSince for test mocks.
func LowestSinceByVariantsFromLowest(
	ctx context.Context,
	lowestFn func(context.Context, string, string, string, time.Time) (*pricing.PriceSnapshot, error),
	variantIDs []string,
	currency, storeID string,
	since time.Time,
) (map[string]*pricing.PriceSnapshot, error) {
	if len(variantIDs) == 0 {
		return nil, nil
	}
	out := make(map[string]*pricing.PriceSnapshot, len(variantIDs))
	for _, variantID := range variantIDs {
		snap, err := lowestFn(ctx, variantID, currency, storeID, since)
		if err != nil {
			return nil, err
		}
		if snap != nil {
			out[variantID] = snap
		}
	}
	return out, nil
}
