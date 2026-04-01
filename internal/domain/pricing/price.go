package pricing

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Price represents a base price for a variant in a specific currency.
type Price struct {
	ID        string
	VariantID string
	Amount    shared.Money
	CreatedAt time.Time
}

// NewPrice creates a Price with the required fields.
func NewPrice(id, variantID string, amount shared.Money) (Price, error) {
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
		Amount:    amount,
		CreatedAt: time.Now().UTC(),
	}, nil
}
