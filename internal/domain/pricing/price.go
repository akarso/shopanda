package pricing

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Price represents a base price for a variant in a specific currency,
// optionally scoped to a store. An empty StoreID means the global/default price.
type Price struct {
	ID        string
	VariantID string
	StoreID   string
	Amount    shared.Money
	CreatedAt time.Time
}

// NewPrice creates a Price with the required fields.
// storeID may be empty to represent the global/default price.
func NewPrice(id, variantID, storeID string, amount shared.Money) (Price, error) {
	if id == "" {
		return Price{}, errors.New("price id must not be empty")
	}
	if variantID == "" {
		return Price{}, errors.New("price variant_id must not be empty")
	}
	if amount.Currency() == "" {
		return Price{}, errors.New("price amount must have a valid currency")
	}
	if !amount.IsPositive() {
		return Price{}, errors.New("price amount must be positive")
	}
	return Price{
		ID:        id,
		VariantID: variantID,
		StoreID:   storeID,
		Amount:    amount,
		CreatedAt: time.Now().UTC(),
	}, nil
}
