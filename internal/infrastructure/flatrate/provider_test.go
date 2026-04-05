package flatrate_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/infrastructure/flatrate"
)

func TestProvider_Method(t *testing.T) {
	cost := shared.MustNewMoney(500, "USD")
	p := flatrate.NewProvider(cost)

	if got := p.Method(); got != shipping.MethodFlatRate {
		t.Fatalf("Method() = %q, want %q", got, shipping.MethodFlatRate)
	}
}

func TestProvider_CalculateRate(t *testing.T) {
	cost := shared.MustNewMoney(500, "USD")
	p := flatrate.NewProvider(cost)

	rate, err := p.CalculateRate(context.Background(), "order-1", "USD", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rate.Cost.Amount() != 500 {
		t.Fatalf("Cost.Amount() = %d, want 500", rate.Cost.Amount())
	}
	if rate.Cost.Currency() != "USD" {
		t.Fatalf("Cost.Currency() = %q, want %q", rate.Cost.Currency(), "USD")
	}
	if rate.ProviderRef != "flat_rate:order-1" {
		t.Fatalf("ProviderRef = %q, want %q", rate.ProviderRef, "flat_rate:order-1")
	}
	if rate.Label != "Flat Rate Shipping" {
		t.Fatalf("Label = %q, want %q", rate.Label, "Flat Rate Shipping")
	}
}

func TestProvider_CalculateRate_CurrencyMismatch(t *testing.T) {
	cost := shared.MustNewMoney(500, "USD")
	p := flatrate.NewProvider(cost)

	_, err := p.CalculateRate(context.Background(), "order-1", "EUR", 3)
	if err == nil {
		t.Fatal("expected error for currency mismatch, got nil")
	}
}

func TestProvider_CalculateRate_IgnoresItemCount(t *testing.T) {
	cost := shared.MustNewMoney(999, "GBP")
	p := flatrate.NewProvider(cost)

	rate1, err := p.CalculateRate(context.Background(), "order-1", "GBP", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rate2, err := p.CalculateRate(context.Background(), "order-1", "GBP", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rate1.Cost.Amount() != rate2.Cost.Amount() {
		t.Fatalf("rates differ: %d vs %d", rate1.Cost.Amount(), rate2.Cost.Amount())
	}
}
