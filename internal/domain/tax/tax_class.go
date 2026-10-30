package tax

import "errors"

// Well-known tax class codes used by rate lookup and catalog derivation.
const (
	TaxClassStandard = "standard"
	TaxClassDigital  = "digital"
	TaxClassReduced  = "reduced"
	TaxClassZero     = "zero"
)

// TaxClass classifies products for tax rate lookup (e.g. "standard", "reduced", "zero").
type TaxClass struct {
	Code string
}

// NewTaxClass creates a TaxClass with the given code.
func NewTaxClass(code string) (TaxClass, error) {
	if code == "" {
		return TaxClass{}, errors.New("tax class code must not be empty")
	}
	return TaxClass{Code: code}, nil
}

// IsKnownClass reports whether code is a recognized rate-table class.
// Empty string is never known. Integrators may still persist other class
// codes in tax_rates; Meta overrides must use a known code.
func IsKnownClass(code string) bool {
	switch code {
	case TaxClassStandard, TaxClassDigital, TaxClassReduced, TaxClassZero:
		return true
	default:
		return false
	}
}

// MissingRateFailsClosed reports whether class + rate must refuse pricing
// (validation error) instead of treating tax as 0%. Digital fails closed when
// the rate row is missing (nil) or has Rate == 0 — never invent 0% VAT for a
// digital line; intentional zero uses class "zero".
func MissingRateFailsClosed(class string, rate *TaxRate) bool {
	if class != TaxClassDigital {
		return false
	}
	return rate == nil || rate.Rate == 0
}
