package pricing

import "context"

// PriceRepository defines persistence operations for prices.
type PriceRepository interface {
	// FindByVariantAndCurrency returns the price for a variant in the given currency.
	// Returns a nil price and no error when no price exists.
	FindByVariantAndCurrency(ctx context.Context, variantID, currency string) (*Price, error)

	// ListByVariantID returns all prices for a variant (one per currency),
	// ordered ascending by currency code. All implementations must preserve
	// this ordering so callers can rely on the sort.
	ListByVariantID(ctx context.Context, variantID string) ([]Price, error)

	// Upsert creates or updates a price for a variant+currency pair.
	Upsert(ctx context.Context, p *Price) error
}
