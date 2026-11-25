package voucher

import (
	"errors"
	"fmt"
	"strings"
)

// Settings marks a virtual product as a voucher and configures issuance.
// A missing row, or Enabled=false, means the product is not a voucher.
type Settings struct {
	ProductID      string
	Enabled        bool
	CodesPerUnit   int // default 1
	MaxRedemptions int // default 1 — per issued code
}

// DefaultSettings returns enabled settings with single-use / one code per unit.
func DefaultSettings(productID string) Settings {
	return Settings{
		ProductID:      strings.TrimSpace(productID),
		Enabled:        true,
		CodesPerUnit:   1,
		MaxRedemptions: 1,
	}
}

// Validate checks settings constraints.
func (s *Settings) Validate() error {
	if s == nil {
		return errors.New("voucher settings must not be nil")
	}
	s.ProductID = strings.TrimSpace(s.ProductID)
	if s.ProductID == "" {
		return errors.New("voucher settings: product id must not be empty")
	}
	if s.CodesPerUnit < 1 {
		return errors.New("voucher settings: codes_per_unit must be at least 1")
	}
	if s.CodesPerUnit > MaxCodesPerUnit {
		return fmt.Errorf("voucher settings: codes_per_unit must not exceed %d", MaxCodesPerUnit)
	}
	if s.MaxRedemptions < 1 {
		return errors.New("voucher settings: max_redemptions must be at least 1")
	}
	if s.MaxRedemptions > MaxRedemptionsCap {
		return fmt.Errorf("voucher settings: max_redemptions must not exceed %d", MaxRedemptionsCap)
	}
	return nil
}

// CodesForQuantity returns how many codes to issue for a line quantity.
func (s *Settings) CodesForQuantity(qty int) (int, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if !s.Enabled {
		return 0, ErrNotEnabled
	}
	if qty < 1 {
		return 0, errors.New("voucher settings: quantity must be at least 1")
	}
	n := s.CodesPerUnit * qty
	if n > MaxCodesPerUnit*100 { // soft cap for a single line
		return 0, fmt.Errorf("voucher settings: issuing %d codes exceeds line cap", n)
	}
	return n, nil
}
