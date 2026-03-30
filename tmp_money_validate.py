#!/usr/bin/env python3
"""Update Money type with currency validation and rewrite tests."""
import os

BASE = "/Users/akarso/_sites/projects/shopanda"
files = {}

files["internal/domain/shared/money.go"] = """\
package shared

import (
\t"fmt"
\t"regexp"
)

// currencyRegex matches ISO 4217 alphabetic currency codes (3 uppercase letters).
var currencyRegex = regexp.MustCompile(`^[A-Z]{3}$`)

// Money represents a monetary value in the smallest currency unit (e.g. cents).
type Money struct {
\tamount   int64  // smallest currency unit
\tcurrency string // ISO 4217 (e.g. "EUR", "USD")
}

// NewMoney creates a Money value. Returns an error if the currency code is invalid.
func NewMoney(amount int64, currency string) (Money, error) {
\tif !isValidCurrency(currency) {
\t\treturn Money{}, fmt.Errorf("money: invalid currency code: %q", currency)
\t}
\treturn Money{amount: amount, currency: currency}, nil
}

// Zero returns a zero Money value for the given currency.
// Returns an error if the currency code is invalid.
func Zero(currency string) (Money, error) {
\tif !isValidCurrency(currency) {
\t\treturn Money{}, fmt.Errorf("money: invalid currency code: %q", currency)
\t}
\treturn Money{amount: 0, currency: currency}, nil
}

// MustNewMoney creates a Money value, panicking on invalid currency.
// Intended for use in tests and initialization with known-good values.
func MustNewMoney(amount int64, currency string) Money {
\tm, err := NewMoney(amount, currency)
\tif err != nil {
\t\tpanic(err)
\t}
\treturn m
}

// MustZero returns a zero Money value, panicking on invalid currency.
func MustZero(currency string) Money {
\tm, err := Zero(currency)
\tif err != nil {
\t\tpanic(err)
\t}
\treturn m
}

// Amount returns the monetary amount in the smallest currency unit.
func (m Money) Amount() int64 {
\treturn m.amount
}

// Currency returns the ISO 4217 currency code.
func (m Money) Currency() string {
\treturn m.currency
}

// Add returns the sum of two Money values. Panics on currency mismatch.
func (m Money) Add(other Money) Money {
\tm.mustMatch(other)
\treturn Money{amount: m.amount + other.amount, currency: m.currency}
}

// Sub returns the difference of two Money values. Panics on currency mismatch.
func (m Money) Sub(other Money) Money {
\tm.mustMatch(other)
\treturn Money{amount: m.amount - other.amount, currency: m.currency}
}

// Mul returns the Money value multiplied by a quantity.
func (m Money) Mul(qty int64) Money {
\treturn Money{amount: m.amount * qty, currency: m.currency}
}

// IsZero returns true if the amount is zero.
func (m Money) IsZero() bool {
\treturn m.amount == 0
}

// IsPositive returns true if the amount is greater than zero.
func (m Money) IsPositive() bool {
\treturn m.amount > 0
}

// IsNegative returns true if the amount is less than zero.
func (m Money) IsNegative() bool {
\treturn m.amount < 0
}

// Equal returns true if both amount and currency match.
func (m Money) Equal(other Money) bool {
\treturn m.amount == other.amount && m.currency == other.currency
}

// String returns a human-readable representation (e.g. "1299 EUR").
func (m Money) String() string {
\treturn fmt.Sprintf("%d %s", m.amount, m.currency)
}

func (m Money) mustMatch(other Money) {
\tif m.currency != other.currency {
\t\tpanic(fmt.Sprintf("money: currency mismatch: %s vs %s", m.currency, other.currency))
\t}
}

// isValidCurrency returns true if code is a 3-letter uppercase ASCII string.
func isValidCurrency(code string) bool {
\treturn currencyRegex.MatchString(code)
}
"""

files["internal/domain/shared/money_test.go"] = """\
package shared_test

import (
\t"testing"

\t"github.com/akarso/shopanda/internal/domain/shared"
)

func mustMoney(t *testing.T, amount int64, currency string) shared.Money {
\tt.Helper()
\tm, err := shared.NewMoney(amount, currency)
\tif err != nil {
\t\tt.Fatalf("NewMoney(%d, %q): %v", amount, currency, err)
\t}
\treturn m
}

func mustZero(t *testing.T, currency string) shared.Money {
\tt.Helper()
\tm, err := shared.Zero(currency)
\tif err != nil {
\t\tt.Fatalf("Zero(%q): %v", currency, err)
\t}
\treturn m
}

func TestNewMoney(t *testing.T) {
\tm := mustMoney(t, 1299, "EUR")
\tif m.Amount() != 1299 {
\t\tt.Errorf("Amount() = %d, want 1299", m.Amount())
\t}
\tif m.Currency() != "EUR" {
\t\tt.Errorf("Currency() = %q, want EUR", m.Currency())
\t}
}

func TestNewMoney_InvalidCurrency(t *testing.T) {
\ttests := []struct {
\t\tname     string
\t\tcurrency string
\t}{
\t\t{"empty", ""},
\t\t{"lowercase", "eur"},
\t\t{"too short", "EU"},
\t\t{"too long", "EURO"},
\t\t{"digits", "123"},
\t\t{"mixed", "E1R"},
\t}
\tfor _, tc := range tests {
\t\tt.Run(tc.name, func(t *testing.T) {
\t\t\t_, err := shared.NewMoney(100, tc.currency)
\t\t\tif err == nil {
\t\t\t\tt.Errorf("NewMoney(100, %q) should return error", tc.currency)
\t\t\t}
\t\t})
\t}
}

func TestZero(t *testing.T) {
\tm := mustZero(t, "USD")
\tif m.Amount() != 0 {
\t\tt.Errorf("Amount() = %d, want 0", m.Amount())
\t}
\tif m.Currency() != "USD" {
\t\tt.Errorf("Currency() = %q, want USD", m.Currency())
\t}
}

func TestZero_InvalidCurrency(t *testing.T) {
\t_, err := shared.Zero("")
\tif err == nil {
\t\tt.Error("Zero with empty currency should return error")
\t}
}

func TestMoney_Add(t *testing.T) {
\ta := mustMoney(t, 1000, "EUR")
\tb := mustMoney(t, 500, "EUR")
\tresult := a.Add(b)
\tif result.Amount() != 1500 {
\t\tt.Errorf("Add: Amount() = %d, want 1500", result.Amount())
\t}
\tif result.Currency() != "EUR" {
\t\tt.Errorf("Add: Currency() = %q, want EUR", result.Currency())
\t}
}

func TestMoney_Sub(t *testing.T) {
\ta := mustMoney(t, 1000, "EUR")
\tb := mustMoney(t, 300, "EUR")
\tresult := a.Sub(b)
\tif result.Amount() != 700 {
\t\tt.Errorf("Sub: Amount() = %d, want 700", result.Amount())
\t}
}

func TestMoney_Mul(t *testing.T) {
\tm := mustMoney(t, 250, "EUR")
\tresult := m.Mul(3)
\tif result.Amount() != 750 {
\t\tt.Errorf("Mul: Amount() = %d, want 750", result.Amount())
\t}
}

func TestMoney_IsZero(t *testing.T) {
\tif !mustZero(t, "EUR").IsZero() {
\t\tt.Error("Zero should be zero")
\t}
\tif mustMoney(t, 1, "EUR").IsZero() {
\t\tt.Error("1 cent should not be zero")
\t}
}

func TestMoney_IsPositive(t *testing.T) {
\tif !mustMoney(t, 100, "EUR").IsPositive() {
\t\tt.Error("100 should be positive")
\t}
\tif mustZero(t, "EUR").IsPositive() {
\t\tt.Error("0 should not be positive")
\t}
\tif mustMoney(t, -1, "EUR").IsPositive() {
\t\tt.Error("-1 should not be positive")
\t}
}

func TestMoney_IsNegative(t *testing.T) {
\tif !mustMoney(t, -50, "EUR").IsNegative() {
\t\tt.Error("-50 should be negative")
\t}
\tif mustZero(t, "EUR").IsNegative() {
\t\tt.Error("0 should not be negative")
\t}
}

func TestMoney_Equal(t *testing.T) {
\ta := mustMoney(t, 500, "EUR")
\tb := mustMoney(t, 500, "EUR")
\tif !a.Equal(b) {
\t\tt.Error("same amount and currency should be equal")
\t}
\tc := mustMoney(t, 500, "USD")
\tif a.Equal(c) {
\t\tt.Error("different currency should not be equal")
\t}
\td := mustMoney(t, 999, "EUR")
\tif a.Equal(d) {
\t\tt.Error("different amount should not be equal")
\t}
}

func TestMoney_String(t *testing.T) {
\tm := mustMoney(t, 1299, "EUR")
\twant := "1299 EUR"
\tif got := m.String(); got != want {
\t\tt.Errorf("String() = %q, want %q", got, want)
\t}
}

func TestMoney_Add_CurrencyMismatch(t *testing.T) {
\tdefer func() {
\t\tif r := recover(); r == nil {
\t\t\tt.Error("Add with mismatched currencies should panic")
\t\t}
\t}()
\tmustMoney(t, 100, "EUR").Add(mustMoney(t, 100, "USD"))
}

func TestMoney_Sub_CurrencyMismatch(t *testing.T) {
\tdefer func() {
\t\tif r := recover(); r == nil {
\t\t\tt.Error("Sub with mismatched currencies should panic")
\t\t}
\t}()
\tmustMoney(t, 100, "EUR").Sub(mustMoney(t, 100, "USD"))
}
"""

for path, content in files.items():
    full = os.path.join(BASE, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w") as f:
        f.write(content)

print("updated", len(files), "files")
