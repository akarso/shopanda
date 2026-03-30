package shared

import (
	"fmt"
	"regexp"
)

// currencyRegex matches ISO 4217 alphabetic currency codes (3 uppercase letters).
var currencyRegex = regexp.MustCompile(`^[A-Z]{3}$`)

// Money represents a monetary value in the smallest currency unit (e.g. cents).
type Money struct {
	amount   int64  // smallest currency unit
	currency string // ISO 4217 (e.g. "EUR", "USD")
}

// NewMoney creates a Money value. Returns an error if the currency code is invalid.
func NewMoney(amount int64, currency string) (Money, error) {
	if !isValidCurrency(currency) {
		return Money{}, fmt.Errorf("money: invalid currency code: %q", currency)
	}
	return Money{amount: amount, currency: currency}, nil
}

// Zero returns a zero Money value for the given currency.
// Returns an error if the currency code is invalid.
func Zero(currency string) (Money, error) {
	if !isValidCurrency(currency) {
		return Money{}, fmt.Errorf("money: invalid currency code: %q", currency)
	}
	return Money{amount: 0, currency: currency}, nil
}

// MustNewMoney creates a Money value, panicking on invalid currency.
// Intended for use in tests and initialization with known-good values.
func MustNewMoney(amount int64, currency string) Money {
	m, err := NewMoney(amount, currency)
	if err != nil {
		panic(err)
	}
	return m
}

// MustZero returns a zero Money value, panicking on invalid currency.
func MustZero(currency string) Money {
	m, err := Zero(currency)
	if err != nil {
		panic(err)
	}
	return m
}

// Amount returns the monetary amount in the smallest currency unit.
func (m Money) Amount() int64 {
	return m.amount
}

// Currency returns the ISO 4217 currency code.
func (m Money) Currency() string {
	return m.currency
}

// Add returns the sum of two Money values. Panics on currency mismatch.
func (m Money) Add(other Money) Money {
	m.mustMatch(other)
	return Money{amount: m.amount + other.amount, currency: m.currency}
}

// Sub returns the difference of two Money values. Panics on currency mismatch.
func (m Money) Sub(other Money) Money {
	m.mustMatch(other)
	return Money{amount: m.amount - other.amount, currency: m.currency}
}

// Mul returns the Money value multiplied by a quantity.
func (m Money) Mul(qty int64) Money {
	return Money{amount: m.amount * qty, currency: m.currency}
}

// IsZero returns true if the amount is zero.
func (m Money) IsZero() bool {
	return m.amount == 0
}

// IsPositive returns true if the amount is greater than zero.
func (m Money) IsPositive() bool {
	return m.amount > 0
}

// IsNegative returns true if the amount is less than zero.
func (m Money) IsNegative() bool {
	return m.amount < 0
}

// Equal returns true if both amount and currency match.
func (m Money) Equal(other Money) bool {
	return m.amount == other.amount && m.currency == other.currency
}

// String returns a human-readable representation (e.g. "1299 EUR").
func (m Money) String() string {
	return fmt.Sprintf("%d %s", m.amount, m.currency)
}

func (m Money) mustMatch(other Money) {
	if m.currency != other.currency {
		panic(fmt.Sprintf("money: currency mismatch: %s vs %s", m.currency, other.currency))
	}
}

// isValidCurrency returns true if code is a 3-letter uppercase ASCII string.
func isValidCurrency(code string) bool {
	return currencyRegex.MatchString(code)
}
