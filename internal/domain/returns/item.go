package returns

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Item represents a line on a return request linked to an order variant.
type Item struct {
	VariantID string
	SKU       string
	Name      string
	Quantity  int
	UnitPrice shared.Money
	CreatedAt time.Time
}

// NewItem creates a return line item with validation.
func NewItem(variantID, sku, name string, quantity int, unitPrice shared.Money) (Item, error) {
	if variantID == "" {
		return Item{}, errors.New("return item: variant id must not be empty")
	}
	if sku == "" {
		return Item{}, errors.New("return item: sku must not be empty")
	}
	if name == "" {
		return Item{}, errors.New("return item: name must not be empty")
	}
	if quantity <= 0 {
		return Item{}, errors.New("return item: quantity must be positive")
	}
	if unitPrice.IsNegative() {
		return Item{}, errors.New("return item: unit price must not be negative")
	}
	return Item{
		VariantID: variantID,
		SKU:       sku,
		Name:      name,
		Quantity:  quantity,
		UnitPrice: unitPrice,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// LineTotal returns unit_price * quantity.
func (i Item) LineTotal() (shared.Money, error) {
	return i.UnitPrice.MulChecked(int64(i.Quantity))
}
