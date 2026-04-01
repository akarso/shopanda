package pricing

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
)

func TestNewPricingContext(t *testing.T) {
	ctx, err := NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.Currency != "EUR" {
		t.Errorf("currency = %q, want %q", ctx.Currency, "EUR")
	}
	if !ctx.Subtotal.IsZero() {
		t.Error("subtotal should be zero")
	}
	if !ctx.DiscountsTotal.IsZero() {
		t.Error("discounts total should be zero")
	}
	if !ctx.TaxTotal.IsZero() {
		t.Error("tax total should be zero")
	}
	if !ctx.FeesTotal.IsZero() {
		t.Error("fees total should be zero")
	}
	if !ctx.GrandTotal.IsZero() {
		t.Error("grand total should be zero")
	}
	if ctx.Meta == nil {
		t.Error("meta should be initialised")
	}
}

func TestNewPricingContextInvalidCurrency(t *testing.T) {
	_, err := NewPricingContext("invalid")
	if err == nil {
		t.Fatal("expected error for invalid currency")
	}
}

func TestNewPricingItem(t *testing.T) {
	price := shared.MustNewMoney(1299, "EUR")
	item, err := NewPricingItem("variant-1", 3, price)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.VariantID != "variant-1" {
		t.Errorf("variant id = %q, want %q", item.VariantID, "variant-1")
	}
	if item.Quantity != 3 {
		t.Errorf("quantity = %d, want %d", item.Quantity, 3)
	}
	if !item.UnitPrice.Equal(price) {
		t.Errorf("unit price = %v, want %v", item.UnitPrice, price)
	}
	expected := shared.MustNewMoney(3897, "EUR")
	if !item.Total.Equal(expected) {
		t.Errorf("total = %v, want %v", item.Total, expected)
	}
}

func TestNewPricingItemEmptyVariantID(t *testing.T) {
	price := shared.MustNewMoney(100, "EUR")
	_, err := NewPricingItem("", 1, price)
	if err == nil {
		t.Fatal("expected error for empty variant id")
	}
}

func TestNewPricingItemZeroQuantity(t *testing.T) {
	price := shared.MustNewMoney(100, "EUR")
	_, err := NewPricingItem("v1", 0, price)
	if err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

func TestNewPricingItemNegativeQuantity(t *testing.T) {
	price := shared.MustNewMoney(100, "EUR")
	_, err := NewPricingItem("v1", -1, price)
	if err == nil {
		t.Fatal("expected error for negative quantity")
	}
}
