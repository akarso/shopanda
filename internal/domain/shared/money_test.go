package shared_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
)

func mustMoney(t *testing.T, amount int64, currency string) shared.Money {
	t.Helper()
	m, err := shared.NewMoney(amount, currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %q): %v", amount, currency, err)
	}
	return m
}

func mustZero(t *testing.T, currency string) shared.Money {
	t.Helper()
	m, err := shared.Zero(currency)
	if err != nil {
		t.Fatalf("Zero(%q): %v", currency, err)
	}
	return m
}

func TestNewMoney(t *testing.T) {
	m := mustMoney(t, 1299, "EUR")
	if m.Amount() != 1299 {
		t.Errorf("Amount() = %d, want 1299", m.Amount())
	}
	if m.Currency() != "EUR" {
		t.Errorf("Currency() = %q, want EUR", m.Currency())
	}
}

func TestNewMoney_InvalidCurrency(t *testing.T) {
	tests := []struct {
		name     string
		currency string
	}{
		{"empty", ""},
		{"lowercase", "eur"},
		{"too short", "EU"},
		{"too long", "EURO"},
		{"digits", "123"},
		{"mixed", "E1R"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := shared.NewMoney(100, tc.currency)
			if err == nil {
				t.Errorf("NewMoney(100, %q) should return error", tc.currency)
			}
		})
	}
}

func TestZero(t *testing.T) {
	m := mustZero(t, "USD")
	if m.Amount() != 0 {
		t.Errorf("Amount() = %d, want 0", m.Amount())
	}
	if m.Currency() != "USD" {
		t.Errorf("Currency() = %q, want USD", m.Currency())
	}
}

func TestZero_InvalidCurrency(t *testing.T) {
	_, err := shared.Zero("")
	if err == nil {
		t.Error("Zero with empty currency should return error")
	}
}

func TestMoney_Add(t *testing.T) {
	a := mustMoney(t, 1000, "EUR")
	b := mustMoney(t, 500, "EUR")
	result := a.Add(b)
	if result.Amount() != 1500 {
		t.Errorf("Add: Amount() = %d, want 1500", result.Amount())
	}
	if result.Currency() != "EUR" {
		t.Errorf("Add: Currency() = %q, want EUR", result.Currency())
	}
}

func TestMoney_Sub(t *testing.T) {
	a := mustMoney(t, 1000, "EUR")
	b := mustMoney(t, 300, "EUR")
	result := a.Sub(b)
	if result.Amount() != 700 {
		t.Errorf("Sub: Amount() = %d, want 700", result.Amount())
	}
}

func TestMoney_Mul(t *testing.T) {
	m := mustMoney(t, 250, "EUR")
	result := m.Mul(3)
	if result.Amount() != 750 {
		t.Errorf("Mul: Amount() = %d, want 750", result.Amount())
	}
}

func TestMoney_IsZero(t *testing.T) {
	if !mustZero(t, "EUR").IsZero() {
		t.Error("Zero should be zero")
	}
	if mustMoney(t, 1, "EUR").IsZero() {
		t.Error("1 cent should not be zero")
	}
}

func TestMoney_IsPositive(t *testing.T) {
	if !mustMoney(t, 100, "EUR").IsPositive() {
		t.Error("100 should be positive")
	}
	if mustZero(t, "EUR").IsPositive() {
		t.Error("0 should not be positive")
	}
	if mustMoney(t, -1, "EUR").IsPositive() {
		t.Error("-1 should not be positive")
	}
}

func TestMoney_IsNegative(t *testing.T) {
	if !mustMoney(t, -50, "EUR").IsNegative() {
		t.Error("-50 should be negative")
	}
	if mustZero(t, "EUR").IsNegative() {
		t.Error("0 should not be negative")
	}
}

func TestMoney_Equal(t *testing.T) {
	a := mustMoney(t, 500, "EUR")
	b := mustMoney(t, 500, "EUR")
	if !a.Equal(b) {
		t.Error("same amount and currency should be equal")
	}
	c := mustMoney(t, 500, "USD")
	if a.Equal(c) {
		t.Error("different currency should not be equal")
	}
	d := mustMoney(t, 999, "EUR")
	if a.Equal(d) {
		t.Error("different amount should not be equal")
	}
}

func TestMoney_String(t *testing.T) {
	m := mustMoney(t, 1299, "EUR")
	want := "1299 EUR"
	if got := m.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestMoney_Add_CurrencyMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Add with mismatched currencies should panic")
		}
	}()
	mustMoney(t, 100, "EUR").Add(mustMoney(t, 100, "USD"))
}

func TestMoney_Sub_CurrencyMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Sub with mismatched currencies should panic")
		}
	}()
	mustMoney(t, 100, "EUR").Sub(mustMoney(t, 100, "USD"))
}

func TestMoney_MulChecked(t *testing.T) {
	m := mustMoney(t, 250, "EUR")
	result, err := m.MulChecked(3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Amount() != 750 {
		t.Errorf("MulChecked: Amount() = %d, want 750", result.Amount())
	}
}

func TestMoney_MulChecked_Overflow(t *testing.T) {
	m := mustMoney(t, 9_000_000_000_000_000_000, "EUR")
	_, err := m.MulChecked(2)
	if err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestMoney_MulChecked_ZeroQty(t *testing.T) {
	m := mustMoney(t, 9_000_000_000_000_000_000, "EUR")
	result, err := m.MulChecked(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Amount() != 0 {
		t.Errorf("MulChecked(0): Amount() = %d, want 0", result.Amount())
	}
}
