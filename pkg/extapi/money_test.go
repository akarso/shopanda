package extapi_test

import (
	"math"
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
)

func mustMoney(t *testing.T, amount int64, currency string) extapi.Money {
	t.Helper()
	m, err := extapi.NewMoney(amount, currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %q): %v", amount, currency, err)
	}
	return m
}

func TestMoney_MulChecked_NegativeQty(t *testing.T) {
	m := mustMoney(t, 100, "USD")
	result, err := m.MulChecked(-1)
	if err != nil {
		t.Fatalf("MulChecked(-1): unexpected error: %v", err)
	}
	if result.Amount() != -100 {
		t.Fatalf("MulChecked(-1): Amount() = %d, want -100", result.Amount())
	}
}

func TestMoney_MulChecked_NegativeAmountAndQty(t *testing.T) {
	m := mustMoney(t, -250, "USD")
	result, err := m.MulChecked(-3)
	if err != nil {
		t.Fatalf("MulChecked(-3): unexpected error: %v", err)
	}
	if result.Amount() != 750 {
		t.Fatalf("MulChecked(-3): Amount() = %d, want 750", result.Amount())
	}
}

func TestMoney_MulChecked_MinInt64TimesNegativeOne(t *testing.T) {
	m := mustMoney(t, math.MinInt64, "USD")
	if _, err := m.MulChecked(-1); err == nil {
		t.Fatal("MulChecked(-1) on MinInt64: expected overflow error")
	}
}

func TestMoney_MulChecked_NegativeOneAmountTimesMinInt64Qty(t *testing.T) {
	m := mustMoney(t, -1, "USD")
	if _, err := m.MulChecked(math.MinInt64); err == nil {
		t.Fatal("MulChecked(MinInt64): expected overflow error")
	}
}

func TestMoney_MulChecked_PositiveOverflow(t *testing.T) {
	m := mustMoney(t, math.MaxInt64, "USD")
	if _, err := m.MulChecked(2); err == nil {
		t.Fatal("MulChecked(2) on MaxInt64: expected overflow error")
	}
}

func TestMoney_MulChecked_ZeroQty(t *testing.T) {
	m := mustMoney(t, math.MaxInt64, "USD")
	result, err := m.MulChecked(0)
	if err != nil {
		t.Fatalf("MulChecked(0): unexpected error: %v", err)
	}
	if result.Amount() != 0 {
		t.Fatalf("MulChecked(0): Amount() = %d, want 0", result.Amount())
	}
}

func TestMoney_Add_CurrencyMismatch(t *testing.T) {
	a := mustMoney(t, 100, "USD")
	b := mustMoney(t, 100, "EUR")
	if _, err := a.Add(b); err == nil {
		t.Fatal("Add: expected error on currency mismatch")
	}
}

func TestMoney_Add_Success(t *testing.T) {
	a := mustMoney(t, 100, "USD")
	b := mustMoney(t, 200, "USD")
	result, err := a.Add(b)
	if err != nil {
		t.Fatalf("Add: unexpected error: %v", err)
	}
	if result.Amount() != 300 {
		t.Fatalf("Add: Amount() = %d, want 300", result.Amount())
	}
}

func TestMoney_Add_Overflow(t *testing.T) {
	a := mustMoney(t, math.MaxInt64, "USD")
	b := mustMoney(t, 1, "USD")
	if _, err := a.Add(b); err == nil {
		t.Fatal("Add: expected overflow error")
	}
}

func TestMoney_Sub_CurrencyMismatch(t *testing.T) {
	a := mustMoney(t, 100, "USD")
	b := mustMoney(t, 100, "EUR")
	if _, err := a.Sub(b); err == nil {
		t.Fatal("Sub: expected error on currency mismatch")
	}
}

func TestMoney_Sub_Success(t *testing.T) {
	a := mustMoney(t, 300, "USD")
	b := mustMoney(t, 100, "USD")
	result, err := a.Sub(b)
	if err != nil {
		t.Fatalf("Sub: unexpected error: %v", err)
	}
	if result.Amount() != 200 {
		t.Fatalf("Sub: Amount() = %d, want 200", result.Amount())
	}
}

func TestMoney_Sub_Overflow(t *testing.T) {
	a := mustMoney(t, math.MinInt64, "USD")
	b := mustMoney(t, 1, "USD")
	if _, err := a.Sub(b); err == nil {
		t.Fatal("Sub: expected overflow error")
	}
}
