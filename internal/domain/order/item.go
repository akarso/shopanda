package order

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Item represents a snapshot of a purchased line item.
// Once created, items are immutable — prices are captured at order time.
type Item struct {
	VariantID string
	SKU       string
	Name      string
	Quantity  int
	UnitPrice shared.Money
	// Restockable is snapshotted when the line is created. NewItem defaults
	// it to true, the physical case. Virtual and downloadable products (the
	// cart parent or an exploded leaf) are set false by checkout, as is a
	// non-exploded dynamic-stock bundle parent (stock was reserved on the
	// leaves). Receive trusts this flag and does not re-read the live product.
	Restockable bool
	CreatedAt   time.Time
}

// NewItem creates an Item with validation.
func NewItem(variantID, sku, name string, quantity int, unitPrice shared.Money) (Item, error) {
	if variantID == "" {
		return Item{}, errors.New("order item: variant id must not be empty")
	}
	if sku == "" {
		return Item{}, errors.New("order item: sku must not be empty")
	}
	if name == "" {
		return Item{}, errors.New("order item: name must not be empty")
	}
	if quantity <= 0 {
		return Item{}, errors.New("order item: quantity must be positive")
	}
	if unitPrice.IsNegative() {
		return Item{}, errors.New("order item: unit price must be non-negative")
	}
	return Item{
		VariantID:   variantID,
		SKU:         sku,
		Name:        name,
		Quantity:    quantity,
		UnitPrice:   unitPrice,
		Restockable: true,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

// LineTotal returns the total price for this line item (unit_price * quantity).
func (i Item) LineTotal() (shared.Money, error) {
	return i.UnitPrice.MulChecked(int64(i.Quantity))
}
