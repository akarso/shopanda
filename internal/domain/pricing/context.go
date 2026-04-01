package pricing

import (
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// PricingContext is the central object passed through the pricing pipeline.
type PricingContext struct {
	Currency string

	Items []PricingItem

	Subtotal       shared.Money
	DiscountsTotal shared.Money
	TaxTotal       shared.Money
	FeesTotal      shared.Money
	GrandTotal     shared.Money

	Adjustments []Adjustment

	Meta map[string]interface{}
}

// NewPricingContext creates a PricingContext for the given currency.
// All monetary fields are initialised to zero in that currency.
func NewPricingContext(currency string) (PricingContext, error) {
	zero, err := shared.Zero(currency)
	if err != nil {
		return PricingContext{}, fmt.Errorf("pricing context: %w", err)
	}
	return PricingContext{
		Currency:       currency,
		Subtotal:       zero,
		DiscountsTotal: zero,
		TaxTotal:       zero,
		FeesTotal:      zero,
		GrandTotal:     zero,
		Meta:           make(map[string]interface{}),
	}, nil
}

// PricingItem represents a single line item in the pricing context.
type PricingItem struct {
	VariantID string
	Quantity  int

	UnitPrice shared.Money
	Total     shared.Money

	Adjustments []Adjustment
}

// NewPricingItem creates a PricingItem with the required fields.
// Total is computed as UnitPrice * Quantity.
func NewPricingItem(variantID string, qty int, unitPrice shared.Money) (PricingItem, error) {
	if variantID == "" {
		return PricingItem{}, errors.New("pricing item: variant id must not be empty")
	}
	if qty <= 0 {
		return PricingItem{}, errors.New("pricing item: quantity must be greater than zero")
	}
	return PricingItem{
		VariantID: variantID,
		Quantity:  qty,
		UnitPrice: unitPrice,
		Total:     unitPrice.Mul(int64(qty)),
	}, nil
}
