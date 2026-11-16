package extapi

import (
	"errors"
	"fmt"
	"math"
	"regexp"
)

var currencyRegex = regexp.MustCompile(`^[A-Z]{3}$`)

// Money is a monetary value in the smallest currency unit (e.g. cents).
// Public mirror of the core money type for product plugins.
type Money struct {
	amount   int64
	currency string
}

// NewMoney creates a Money value. Returns an error if the currency code is invalid.
func NewMoney(amount int64, currency string) (Money, error) {
	if !currencyRegex.MatchString(currency) {
		return Money{}, fmt.Errorf("money: invalid currency code: %q", currency)
	}
	return Money{amount: amount, currency: currency}, nil
}

// MustNewMoney creates a Money value, panicking on invalid currency.
func MustNewMoney(amount int64, currency string) Money {
	m, err := NewMoney(amount, currency)
	if err != nil {
		panic(err)
	}
	return m
}

// Zero returns a zero Money value for the given currency.
func Zero(currency string) (Money, error) {
	return NewMoney(0, currency)
}

// Amount returns the monetary amount in the smallest currency unit.
func (m Money) Amount() int64 { return m.amount }

// Currency returns the ISO 4217 currency code.
func (m Money) Currency() string { return m.currency }

// Add returns the sum of two Money values. Returns an error on currency
// mismatch or int64 overflow instead of panicking, since plugin-supplied
// amounts are not trusted the way core invariants are.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("money: currency mismatch %q vs %q", m.currency, other.currency)
	}
	sum := m.amount + other.amount
	if (other.amount > 0 && sum < m.amount) || (other.amount < 0 && sum > m.amount) {
		return Money{}, errors.New("money: addition overflow")
	}
	return Money{amount: sum, currency: m.currency}, nil
}

// MulChecked multiplies by qty with overflow checking. Handles negative qty
// (including math.MinInt64) correctly: the product is validated by dividing
// back and comparing, with an explicit guard for the MinInt64 * -1 case that
// would otherwise wrap around and defeat that check.
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

// Equal reports whether amount and currency match.
func (m Money) Equal(other Money) bool {
	return m.amount == other.amount && m.currency == other.currency
}

// Sub returns the difference of two Money values. Returns an error on
// currency mismatch or int64 overflow instead of panicking.
func (m Money) Sub(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("money: currency mismatch %q vs %q", m.currency, other.currency)
	}
	diff := m.amount - other.amount
	if (other.amount < 0 && diff < m.amount) || (other.amount > 0 && diff > m.amount) {
		return Money{}, errors.New("money: subtraction overflow")
	}
	return Money{amount: diff, currency: m.currency}, nil
}

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.amount == 0 }

// IsPositive reports whether the amount is greater than zero.
func (m Money) IsPositive() bool { return m.amount > 0 }

// IsNegative reports whether the amount is less than zero.
func (m Money) IsNegative() bool { return m.amount < 0 }
