package cart

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Item represents a line item in a cart.
type Item struct {
	VariantID string
	Quantity  int
	UnitPrice shared.Money
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewItem creates an Item with validation.
func NewItem(variantID string, quantity int, unitPrice shared.Money) (Item, error) {
	if variantID == "" {
		return Item{}, errors.New("cart item: variant id must not be empty")
	}
	if quantity <= 0 {
		return Item{}, errors.New("cart item: quantity must be positive")
	}
	if unitPrice.IsNegative() {
		return Item{}, errors.New("cart item: unit price must be non-negative")
	}
	now := time.Now().UTC()
	return Item{
		VariantID: variantID,
		Quantity:  quantity,
		UnitPrice: unitPrice,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// LineTotal returns the total price for this line item (unit_price * quantity).
func (i Item) LineTotal() (shared.Money, error) {
	return i.UnitPrice.MulChecked(int64(i.Quantity))
}
