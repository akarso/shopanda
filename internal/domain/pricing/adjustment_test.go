package pricing

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
)

func TestNewAdjustment(t *testing.T) {
	amount := shared.MustNewMoney(500, "EUR")
	adj, err := NewAdjustment(AdjustmentDiscount, "PROMO10", amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adj.Type != AdjustmentDiscount {
		t.Errorf("type = %q, want %q", adj.Type, AdjustmentDiscount)
	}
	if adj.Code != "PROMO10" {
		t.Errorf("code = %q, want %q", adj.Code, "PROMO10")
	}
	if !adj.Amount.Equal(amount) {
		t.Errorf("amount = %v, want %v", adj.Amount, amount)
	}
	if adj.Meta == nil {
		t.Error("meta should be initialised")
	}
}

func TestNewAdjustmentInvalidType(t *testing.T) {
	amount := shared.MustNewMoney(100, "EUR")
	_, err := NewAdjustment("bogus", "X", amount)
	if err == nil {
		t.Fatal("expected error for invalid adjustment type")
	}
}

func TestNewAdjustmentEmptyCode(t *testing.T) {
	amount := shared.MustNewMoney(100, "EUR")
	_, err := NewAdjustment(AdjustmentTax, "", amount)
	if err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestAdjustmentTypeIsValid(t *testing.T) {
	valid := []AdjustmentType{AdjustmentDiscount, AdjustmentTax, AdjustmentFee}
	for _, v := range valid {
		if !v.IsValid() {
			t.Errorf("%q should be valid", v)
		}
	}
	if AdjustmentType("unknown").IsValid() {
		t.Error("unknown type should be invalid")
	}
}
