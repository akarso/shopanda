package inventory

import (
	"errors"
	"time"
)

// StockEntry represents the current stock level for a variant.
type StockEntry struct {
	VariantID string
	Quantity  int
	UpdatedAt time.Time
}

// NewStockEntry creates a StockEntry with validation.
func NewStockEntry(variantID string, quantity int) (StockEntry, error) {
	if variantID == "" {
		return StockEntry{}, errors.New("stock: variant id must not be empty")
	}
	if quantity < 0 {
		return StockEntry{}, errors.New("stock: quantity must not be negative")
	}
	return StockEntry{
		VariantID: variantID,
		Quantity:  quantity,
		UpdatedAt: time.Now().UTC(),
	}, nil
}

// IsAvailable returns true if the stock quantity is greater than zero.
func (s StockEntry) IsAvailable() bool {
	return s.Quantity > 0
}

// HasStock returns true if the stock has at least the requested quantity.
// Negative needed values are considered invalid and return false.
func (s StockEntry) HasStock(needed int) bool {
	if needed < 0 {
		return false
	}
	return s.Quantity >= needed
}
