package tax

import "errors"

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
