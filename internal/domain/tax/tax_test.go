package tax_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/tax"
)

func TestNewTaxClass(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		tc, err := tax.NewTaxClass("standard")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tc.Code != "standard" {
			t.Fatalf("got code %q, want %q", tc.Code, "standard")
		}
	})

	t.Run("empty code", func(t *testing.T) {
		_, err := tax.NewTaxClass("")
		if err == nil {
			t.Fatal("expected error for empty code")
		}
	})
}

func TestNewTaxRate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		r, err := tax.NewTaxRate("f47ac10b-58cc-4372-a567-0e02b2c3d479", "DE", "standard", 1900)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Country != "DE" {
			t.Fatalf("got country %q, want %q", r.Country, "DE")
		}
		if r.Class != "standard" {
			t.Fatalf("got class %q, want %q", r.Class, "standard")
		}
		if r.Rate != 1900 {
			t.Fatalf("got rate %d, want %d", r.Rate, 1900)
		}
	})

	t.Run("empty id", func(t *testing.T) {
		_, err := tax.NewTaxRate("", "DE", "standard", 1900)
		if err == nil {
			t.Fatal("expected error for empty id")
		}
	})

	t.Run("invalid uuid", func(t *testing.T) {
		_, err := tax.NewTaxRate("not-a-uuid", "DE", "standard", 1900)
		if err == nil {
			t.Fatal("expected error for invalid uuid")
		}
	})

	t.Run("invalid country", func(t *testing.T) {
		_, err := tax.NewTaxRate("f47ac10b-58cc-4372-a567-0e02b2c3d479", "germany", "standard", 1900)
		if err == nil {
			t.Fatal("expected error for invalid country code")
		}
	})

	t.Run("empty class", func(t *testing.T) {
		_, err := tax.NewTaxRate("f47ac10b-58cc-4372-a567-0e02b2c3d479", "DE", "", 1900)
		if err == nil {
			t.Fatal("expected error for empty class")
		}
	})

	t.Run("negative rate", func(t *testing.T) {
		_, err := tax.NewTaxRate("f47ac10b-58cc-4372-a567-0e02b2c3d479", "DE", "standard", -100)
		if err == nil {
			t.Fatal("expected error for negative rate")
		}
	})

	t.Run("zero rate allowed", func(t *testing.T) {
		r, err := tax.NewTaxRate("f47ac10b-58cc-4372-a567-0e02b2c3d479", "DE", "zero", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Rate != 0 {
			t.Fatalf("got rate %d, want 0", r.Rate)
		}
	})
}

func TestTaxRate_Percentage(t *testing.T) {
	r := tax.TaxRate{Rate: 2100}
	got := r.Percentage()
	if got != 21.0 {
		t.Fatalf("got %f, want 21.0", got)
	}
}

func TestTaxMode_IsValid(t *testing.T) {
	tests := []struct {
		mode tax.TaxMode
		want bool
	}{
		{tax.ModeExclusive, true},
		{tax.ModeInclusive, true},
		{"bogus", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.mode.IsValid(); got != tt.want {
			t.Errorf("TaxMode(%q).IsValid() = %v, want %v", tt.mode, got, tt.want)
		}
	}
}
