package shared

import "fmt"

// Money represents a monetary value in the smallest currency unit (e.g. cents).
type Money struct {
	Amount   int64  // smallest currency unit
	Currency string // ISO 4217 (e.g. "EUR", "USD")
}

// NewMoney creates a Money value.
func NewMoney(amount int64, currency string) Money {
	return Money{Amount: amount, Currency: currency}
}

// Zero returns a zero Money value for the given currency.
func Zero(currency string) Money {
	return Money{Amount: 0, Currency: currency}
}

// Add returns the sum of two Money values. Panics on currency mismatch.
func (m Money) Add(other Money) Money {
	m.mustMatch(other)
	return Money{Amount: m.Amount + other.Amount, Currency: m.Currency}
}

// Sub returns the difference of two Money values. Panics on currency mismatch.
func (m Money) Sub(other Money) Money {
	m.mustMatch(other)
	return Money{Amount: m.Amount - other.Amount, Currency: m.Currency}
}

// Mul returns the Money value multiplied by a quantity.
func (m Money) Mul(qty int64) Money {
	return Money{Amount: m.Amount * qty, Currency: m.Currency}
}

// IsZero returns true if the amount is zero.
func (m Money) IsZero() bool {
	return m.Amount == 0
}

// IsPositive returns true if the amount is greater than zero.
func (m Money) IsPositive() bool {
	return m.Amount > 0
}

// IsNegative returns true if the amount is less than zero.
func (m Money) IsNegative() bool {
	return m.Amount < 0
}

// Equal returns true if both amount and currency match.
func (m Money) Equal(other Money) bool {
	return m.Amount == other.Amount && m.Currency == other.Currency
}

// String returns a human-readable representation (e.g. "1299 EUR").
func (m Money) String() string {
	return fmt.Sprintf("%d %s", m.Amount, m.Currency)
}

func (m Money) mustMatch(other Money) {
	if m.Currency != other.Currency {
		panic(fmt.Sprintf("money: currency mismatch: %s vs %s", m.Currency, other.Currency))
	}
}
