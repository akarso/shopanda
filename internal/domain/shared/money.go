package shared

import (
	"errors"
	"fmt"
	"math"
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
	if !IsValidCurrency(currency) {
		return Money{}, fmt.Errorf("money: invalid currency code: %q", currency)
	}
	return Money{amount: amount, currency: currency}, nil
}

// Zero returns a zero Money value for the given currency.
// Returns an error if the currency code is invalid.
func Zero(currency string) (Money, error) {
	if !IsValidCurrency(currency) {
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

// Add returns the sum of two Money values. Returns an error on currency
// mismatch or int64 overflow, matching extapi.Money.Add / AddChecked.
func (m Money) Add(other Money) (Money, error) {
	return m.AddChecked(other)
}

// AddChecked returns the sum of two Money values.
// Returns an error on currency mismatch or if the addition would overflow int64.
func (m Money) AddChecked(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("money: currency mismatch: %s vs %s", m.currency, other.currency)
	}
	sum := m.amount + other.amount
	// Overflow: both positive but sum negative, or both negative but sum positive.
	if (other.amount > 0 && sum < m.amount) || (other.amount < 0 && sum > m.amount) {
		return Money{}, errors.New("money: addition overflow")
	}
	return Money{amount: sum, currency: m.currency}, nil
}

// Sub returns the difference of two Money values. Returns an error on
// currency mismatch or int64 overflow, matching extapi.Money.Sub.
func (m Money) Sub(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("money: currency mismatch: %s vs %s", m.currency, other.currency)
	}
	diff := m.amount - other.amount
	if (other.amount < 0 && diff < m.amount) || (other.amount > 0 && diff > m.amount) {
		return Money{}, errors.New("money: subtraction overflow")
	}
	return Money{amount: diff, currency: m.currency}, nil
}

// Mul returns the Money value multiplied by a quantity.
func (m Money) Mul(qty int64) Money {
	return Money{amount: m.amount * qty, currency: m.currency}
}

// MulChecked returns the Money value multiplied by a quantity.
// Returns an error if the multiplication would overflow int64. Handles
// negative qty (including math.MinInt64) correctly: the product is
// validated by dividing back and comparing, with an explicit guard for the
// MinInt64 * -1 case that would otherwise wrap around and defeat that check.
func (m Money) MulChecked(qty int64) (Money, error) {
	if qty == 0 || m.amount == 0 {
		return Money{amount: 0, currency: m.currency}, nil
	}
	if (m.amount == -1 && qty == math.MinInt64) || (qty == -1 && m.amount == math.MinInt64) {
		return Money{}, errors.New("money: multiplication overflow")
	}
	result := m.amount * qty
	if result/qty != m.amount {
		return Money{}, errors.New("money: multiplication overflow")
	}
	return Money{amount: result, currency: m.currency}, nil
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

// IsValidCurrency returns true if code is a 3-letter uppercase ASCII string.
func IsValidCurrency(code string) bool {
	return currencyRegex.MatchString(code)
}
