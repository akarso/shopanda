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

	// LowestSinceByVariants returns the lowest snapshot per variant in the window,
	// keyed by variant ID. Variants with no snapshots in the window are omitted.
	LowestSinceByVariants(ctx context.Context, variantIDs []string, currency, storeID string, since time.Time) (map[string]*PriceSnapshot, error)
}
