package pricing

import (
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// AdjustmentType classifies a price adjustment.
type AdjustmentType string

const (
	AdjustmentDiscount AdjustmentType = "discount"
	AdjustmentTax      AdjustmentType = "tax"
	AdjustmentFee      AdjustmentType = "fee"
)

// IsValid returns true if t is a recognised adjustment type.
func (t AdjustmentType) IsValid() bool {
	switch t {
	case AdjustmentDiscount, AdjustmentTax, AdjustmentFee:
		return true
	}
	return false
}

// Adjustment represents a modification to a price (discount, tax, or fee).
type Adjustment struct {
	Type        AdjustmentType
	Code        string
	Description string
	Amount      shared.Money
	Included    bool
	Meta        map[string]interface{}
}

// NewAdjustment creates an Adjustment with the required fields.
func NewAdjustment(typ AdjustmentType, code string, amount shared.Money) (Adjustment, error) {
	if !typ.IsValid() {
		return Adjustment{}, fmt.Errorf("adjustment: invalid type: %q", typ)
	}
	if code == "" {
		return Adjustment{}, errors.New("adjustment code must not be empty")
	}
	return Adjustment{
		Type:   typ,
		Code:   code,
		Amount: amount,
		Meta:   make(map[string]interface{}),
	}, nil
}
