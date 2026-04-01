package pricing

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestNewPrice(t *testing.T) {
	amount := shared.MustNewMoney(1299, "EUR")
	p, err := NewPrice(id.New(), "variant-1", amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.VariantID != "variant-1" {
		t.Errorf("variant id = %q, want %q", p.VariantID, "variant-1")
	}
	if !p.Amount.Equal(amount) {
		t.Errorf("amount = %v, want %v", p.Amount, amount)
	}
	if p.CreatedAt.IsZero() {
		t.Error("created_at should be set")
	}
}

func TestNewPriceEmptyID(t *testing.T) {
	amount := shared.MustNewMoney(100, "EUR")
	_, err := NewPrice("", "v1", amount)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewPriceEmptyVariantID(t *testing.T) {
	amount := shared.MustNewMoney(100, "EUR")
	_, err := NewPrice(id.New(), "", amount)
	if err == nil {
		t.Fatal("expected error for empty variant id")
	}
}

func TestNewPriceZeroAmount(t *testing.T) {
	amount := shared.MustNewMoney(0, "EUR")
	_, err := NewPrice(id.New(), "v1", amount)
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
}

func TestNewPriceZeroValueMoney(t *testing.T) {
	var amount shared.Money // zero value — empty currency
	_, err := NewPrice(id.New(), "v1", amount)
	if err == nil {
		t.Fatal("expected error for zero-value Money")
	}
}

func TestNewPriceNegativeAmount(t *testing.T) {
	amount := shared.MustNewMoney(-100, "EUR")
	_, err := NewPrice(id.New(), "v1", amount)
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
}
