package cart

import (
	"errors"
	"strings"
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
	// AssignmentParentProductID and AssignmentDeferred persist the
	// purchase-time assignment context on the line (PR-1068). Empty
	// means the line has no stored context; callers then fall back to
	// the request context. PR-1070 writes these via HTTP.
	AssignmentParentProductID string
	AssignmentDeferred        bool
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

// AssignmentParent returns the trimmed stored parent product id.
func (i Item) AssignmentParent() string {
	return strings.TrimSpace(i.AssignmentParentProductID)
}

// HasStoredAssignment reports whether the line carries a parent or deferred flag.
func (i Item) HasStoredAssignment() bool {
	return i.AssignmentDeferred || i.AssignmentParent() != ""
}
