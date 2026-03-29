package shared_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
)

func TestNewMoney(t *testing.T) {
	m := shared.NewMoney(1299, "EUR")
	if m.Amount != 1299 {
		t.Errorf("Amount = %d, want 1299", m.Amount)
	}
	if m.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", m.Currency)
	}
}

func TestZero(t *testing.T) {
	m := shared.Zero("USD")
	if m.Amount != 0 {
		t.Errorf("Amount = %d, want 0", m.Amount)
	}
	if m.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", m.Currency)
	}
}

func TestMoney_Add(t *testing.T) {
	a := shared.NewMoney(1000, "EUR")
	b := shared.NewMoney(500, "EUR")
	result := a.Add(b)
	if result.Amount != 1500 {
		t.Errorf("Add: Amount = %d, want 1500", result.Amount)
	}
	if result.Currency != "EUR" {
		t.Errorf("Add: Currency = %q, want EUR", result.Currency)
	}
}

func TestMoney_Sub(t *testing.T) {
	a := shared.NewMoney(1000, "EUR")
	b := shared.NewMoney(300, "EUR")
	result := a.Sub(b)
	if result.Amount != 700 {
		t.Errorf("Sub: Amount = %d, want 700", result.Amount)
	}
}

func TestMoney_Mul(t *testing.T) {
	m := shared.NewMoney(250, "EUR")
	result := m.Mul(3)
	if result.Amount != 750 {
		t.Errorf("Mul: Amount = %d, want 750", result.Amount)
	}
}

func TestMoney_IsZero(t *testing.T) {
	if !shared.Zero("EUR").IsZero() {
		t.Error("Zero should be zero")
	}
	if shared.NewMoney(1, "EUR").IsZero() {
		t.Error("1 cent should not be zero")
	}
}

func TestMoney_IsPositive(t *testing.T) {
	if !shared.NewMoney(100, "EUR").IsPositive() {
		t.Error("100 should be positive")
	}
	if shared.Zero("EUR").IsPositive() {
		t.Error("0 should not be positive")
	}
	if shared.NewMoney(-1, "EUR").IsPositive() {
		t.Error("-1 should not be positive")
	}
}

func TestMoney_IsNegative(t *testing.T) {
	if !shared.NewMoney(-50, "EUR").IsNegative() {
		t.Error("-50 should be negative")
	}
	if shared.Zero("EUR").IsNegative() {
		t.Error("0 should not be negative")
	}
}

func TestMoney_Equal(t *testing.T) {
	a := shared.NewMoney(500, "EUR")
	b := shared.NewMoney(500, "EUR")
	if !a.Equal(b) {
		t.Error("same amount and currency should be equal")
	}
	c := shared.NewMoney(500, "USD")
	if a.Equal(c) {
		t.Error("different currency should not be equal")
	}
	d := shared.NewMoney(999, "EUR")
	if a.Equal(d) {
		t.Error("different amount should not be equal")
	}
}

func TestMoney_String(t *testing.T) {
	m := shared.NewMoney(1299, "EUR")
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
	shared.NewMoney(100, "EUR").Add(shared.NewMoney(100, "USD"))
}

func TestMoney_Sub_CurrencyMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Sub with mismatched currencies should panic")
		}
	}()
	shared.NewMoney(100, "EUR").Sub(shared.NewMoney(100, "USD"))
}
