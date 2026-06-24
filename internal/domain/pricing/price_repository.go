package pricing

import "context"

// PriceRepository defines persistence operations for prices.
type PriceRepository interface {
	// FindByVariantCurrencyAndStore returns the price for a variant in the given
	// currency and store. An empty storeID means the global/default price.
	// Returns a nil price and no error when no price exists.
	FindByVariantCurrencyAndStore(ctx context.Context, variantID, currency, storeID string) (*Price, error)

	// FindByVariantsCurrencyAndStore returns prices keyed by variant ID for the given
	// currency and store. When storeID is non-empty, variants without a store-scoped
	// row fall back to the global price (empty store_id), matching
	// FindByVariantCurrencyAndStore.
	FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*Price, error)

	// ListByVariantID returns all prices for a variant (one per currency+store),
	// ordered ascending by currency code then store_id. All implementations
	// must preserve this ordering so callers can rely on the sort.
	ListByVariantID(ctx context.Context, variantID string) ([]Price, error)

	// List returns a page of prices ordered by variant_id, currency, store_id.
	// offset must be >= 0, limit must be > 0.
	List(ctx context.Context, offset, limit int) ([]Price, error)

	// Upsert creates or updates a price for a variant+currency+store tuple.
	Upsert(ctx context.Context, p *Price) error
}
