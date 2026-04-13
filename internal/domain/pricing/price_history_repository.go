package pricing

import (
	"context"
	"time"
)

// PriceHistoryRepository defines persistence for price snapshots.
type PriceHistoryRepository interface {
	// Record inserts a new price snapshot.
	Record(ctx context.Context, s *PriceSnapshot) error

	// LowestSince returns the snapshot with the lowest amount for the given
	// variant, currency, and store recorded on or after since.
	// Returns (nil, nil) when no snapshots exist in the window.
	LowestSince(ctx context.Context, variantID, currency, storeID string, since time.Time) (*PriceSnapshot, error)
}
