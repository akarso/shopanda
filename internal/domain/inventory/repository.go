package inventory

import "context"

// StockRepository defines persistence operations for inventory stock.
type StockRepository interface {
	// GetStock returns the stock entry for a variant.
	// Returns a zero-quantity entry (not an error) when no stock record exists.
	GetStock(ctx context.Context, variantID string) (StockEntry, error)

	// SetStock sets the absolute stock quantity for a variant.
	// Creates the record if it does not exist, updates it otherwise.
	SetStock(ctx context.Context, entry *StockEntry) error

	// ListStock returns a page of stock entries ordered by variant_id.
	// offset must be >= 0; limit must be > 0.
	ListStock(ctx context.Context, offset, limit int) ([]StockEntry, error)
}
