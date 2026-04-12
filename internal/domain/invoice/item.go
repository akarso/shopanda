package invoice

import (
	"errors"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Item represents a snapshot of a line item on an invoice or credit note.
type Item struct {
	VariantID string
	SKU       string
	Name      string
	Quantity  int
	UnitPrice shared.Money
}

// validateItemFields checks the basic field constraints shared by NewItem and Validate.
func validateItemFields(variantID, sku, name string, quantity int, unitPrice shared.Money) error {
	if variantID == "" {
		return errors.New("invoice item: variant id must not be empty")
	}
	if sku == "" {
		return errors.New("invoice item: sku must not be empty")
	}
	if name == "" {
		return errors.New("invoice item: name must not be empty")
	}
	if quantity <= 0 {
		return errors.New("invoice item: quantity must be positive")
	}
	if unitPrice.IsNegative() {
		return errors.New("invoice item: unit price must be non-negative")
	}
	return nil
}

// NewItem creates an Item with validation.
func NewItem(variantID, sku, name string, quantity int, unitPrice shared.Money) (Item, error) {
	if err := validateItemFields(variantID, sku, name, quantity, unitPrice); err != nil {
		return Item{}, err
	}
	return Item{
		VariantID: variantID,
		SKU:       sku,
		Name:      name,
		Quantity:  quantity,
		UnitPrice: unitPrice,
	}, nil
}

// Validate checks that the item's fields are well-formed and that its
// currency matches the expected currency. It runs the same checks as NewItem
// plus currency matching, and is intended for validating items hydrated from
// persistence where NewItem was not called.
func (it Item) Validate(currency string) error {
	if err := validateItemFields(it.VariantID, it.SKU, it.Name, it.Quantity, it.UnitPrice); err != nil {
		return err
	}
	if it.UnitPrice.Currency() != currency {
		return errors.New("invoice item: currency mismatch")
	}
	return nil
}

// LineTotal returns the total price for this line item (unit_price * quantity).
func (it Item) LineTotal() (shared.Money, error) {
	return it.UnitPrice.MulChecked(int64(it.Quantity))
}
