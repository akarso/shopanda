package customergroup

import "context"

// GroupPriceRepository persists group-scoped variant prices.
type GroupPriceRepository interface {
	// FindByVariantsGroupCurrencyAndStore returns group prices keyed by variant ID.
	// When storeID is non-empty, variants without a store-scoped row fall back to
	// the global group price (empty store_id).
	FindByVariantsGroupCurrencyAndStore(ctx context.Context, variantIDs []string, groupID, currency, storeID string) (map[string]*GroupPrice, error)

	// FindExactByVariantGroupCurrencyAndStore returns the row for the exact store
	// scope only. (nil, nil) when not found. No global fallback.
	FindExactByVariantGroupCurrencyAndStore(ctx context.Context, variantID, groupID, currency, storeID string) (*GroupPrice, error)

	// FindByVariantGroupCurrencyAndStore returns a single group price row with
	// store-scoped fallback to the global group price when storeID is non-empty.
	FindByVariantGroupCurrencyAndStore(ctx context.Context, variantID, groupID, currency, storeID string) (*GroupPrice, error)

	// Upsert creates or updates a group price for variant+group+currency+store.
	Upsert(ctx context.Context, price *GroupPrice) error

	// Delete removes a group price by ID.
	Delete(ctx context.Context, id string) error
}
