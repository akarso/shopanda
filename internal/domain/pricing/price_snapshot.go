package pricing

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// PriceSnapshot records a point-in-time price for a variant, used to satisfy
// EU Omnibus directive requirements (lowest price in last 30 days).
type PriceSnapshot struct {
	ID         string
	VariantID  string
	StoreID    string
	Amount     shared.Money
	RecordedAt time.Time
}

// NewPriceSnapshot creates a PriceSnapshot with required fields.
func NewPriceSnapshot(id, variantID, storeID string, amount shared.Money) (PriceSnapshot, error) {
	if id == "" {
		return PriceSnapshot{}, errors.New("price snapshot id must not be empty")
	}
	if variantID == "" {
		return PriceSnapshot{}, errors.New("price snapshot variant_id must not be empty")
	}
	if amount.Currency() == "" {
		return PriceSnapshot{}, errors.New("price snapshot amount must have a valid currency")
	}
	if !amount.IsPositive() {
		return PriceSnapshot{}, errors.New("price snapshot amount must be positive")
	}
	return PriceSnapshot{
		ID:         id,
		VariantID:  variantID,
		StoreID:    storeID,
		Amount:     amount,
		RecordedAt: time.Now().UTC(),
	}, nil
}
