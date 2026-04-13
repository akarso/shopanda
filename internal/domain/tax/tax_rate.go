package tax

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/akarso/shopanda/internal/platform/id"
)

// TaxMode determines how tax is applied to a price.
type TaxMode string

const (
	// ModeExclusive means tax is added on top of the price.
	ModeExclusive TaxMode = "exclusive"
	// ModeInclusive means the price already includes tax.
	ModeInclusive TaxMode = "inclusive"
)

// IsValid returns true if m is a recognised tax mode.
func (m TaxMode) IsValid() bool {
	switch m {
	case ModeExclusive, ModeInclusive:
		return true
	}
	return false
}

// countryRegex matches ISO 3166-1 alpha-2 country codes (2 uppercase letters).
var countryRegex = regexp.MustCompile(`^[A-Z]{2}$`)

// TaxRate defines the tax percentage for a country + tax class combination,
// optionally scoped to a store. An empty StoreID means the global/default rate.
// Rate is stored as basis points (e.g. 2100 = 21.00%).
type TaxRate struct {
	ID      string
	Country string
	Class   string
	StoreID string
	Rate    int
}

// NewTaxRate creates a TaxRate with the required fields.
// storeID may be empty to represent the global/default rate.
func NewTaxRate(rateID, country, class, storeID string, rate int) (TaxRate, error) {
	if rateID == "" {
		return TaxRate{}, errors.New("tax rate id must not be empty")
	}
	if !id.IsValid(rateID) {
		return TaxRate{}, errors.New("tax rate id must be a valid UUID")
	}
	if !countryRegex.MatchString(country) {
		return TaxRate{}, fmt.Errorf("tax rate: invalid country code: %q", country)
	}
	if class == "" {
		return TaxRate{}, errors.New("tax rate class must not be empty")
	}
	if rate < 0 {
		return TaxRate{}, errors.New("tax rate must not be negative")
	}
	return TaxRate{
		ID:      rateID,
		Country: country,
		Class:   class,
		StoreID: storeID,
		Rate:    rate,
	}, nil
}

// Percentage returns the rate as a float64 percentage (e.g. 21.00 for 2100 basis points).
func (r TaxRate) Percentage() float64 {
	return float64(r.Rate) / 100.0
}
