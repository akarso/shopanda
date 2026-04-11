package tax_test

import (
	"math"
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/tax"
)

func TestCalculate_Exclusive(t *testing.T) {
	price := shared.MustNewMoney(10000, "EUR")
	rate := tax.TaxRate{ID: "r1", Country: "DE", Class: "standard", Rate: 1900}
	got, err := tax.Calculate(price, rate, tax.ModeExclusive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount() != 1900 {
		t.Fatalf("got amount %d, want 1900", got.Amount())
	}
	if got.Currency() != "EUR" {
		t.Fatalf("got currency %q, want EUR", got.Currency())
	}
}

func TestCalculate_Inclusive(t *testing.T) {
	price := shared.MustNewMoney(11900, "EUR")
	rate := tax.TaxRate{ID: "r1", Country: "DE", Class: "standard", Rate: 1900}
	got, err := tax.Calculate(price, rate, tax.ModeInclusive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount() != 1900 {
		t.Fatalf("got amount %d, want 1900", got.Amount())
	}
}

func TestCalculate_ZeroRate(t *testing.T) {
	price := shared.MustNewMoney(5000, "EUR")
	rate := tax.TaxRate{ID: "r1", Country: "DE", Class: "zero", Rate: 0}
	got, err := tax.Calculate(price, rate, tax.ModeExclusive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount() != 0 {
		t.Fatalf("got amount %d, want 0", got.Amount())
	}
}

func TestCalculate_InvalidMode(t *testing.T) {
	price := shared.MustNewMoney(1000, "EUR")
	rate := tax.TaxRate{ID: "r1", Country: "DE", Class: "standard", Rate: 1900}
	_, err := tax.Calculate(price, rate, "bogus")
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestCalculate_ReducedRate(t *testing.T) {
	price := shared.MustNewMoney(10000, "EUR")
	rate := tax.TaxRate{ID: "r2", Country: "DE", Class: "reduced", Rate: 700}
	got, err := tax.Calculate(price, rate, tax.ModeExclusive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount() != 700 {
		t.Fatalf("got amount %d, want 700", got.Amount())
	}
}

func TestCalculate_InclusiveReduced(t *testing.T) {
	price := shared.MustNewMoney(10700, "EUR")
	rate := tax.TaxRate{ID: "r2", Country: "DE", Class: "reduced", Rate: 700}
	got, err := tax.Calculate(price, rate, tax.ModeInclusive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount() != 700 {
		t.Fatalf("got amount %d, want 700", got.Amount())
	}
}

func TestCalculate_ExclusiveOverflow(t *testing.T) {
	price := shared.MustNewMoney(math.MaxInt64, "EUR")
	rate := tax.TaxRate{ID: "r1", Country: "DE", Class: "standard", Rate: 1900}
	_, err := tax.Calculate(price, rate, tax.ModeExclusive)
	if err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestCalculate_InclusiveOverflow(t *testing.T) {
	price := shared.MustNewMoney(math.MaxInt64, "EUR")
	rate := tax.TaxRate{ID: "r1", Country: "DE", Class: "standard", Rate: 1900}
	_, err := tax.Calculate(price, rate, tax.ModeInclusive)
	if err == nil {
		t.Fatal("expected overflow error")
	}
}
