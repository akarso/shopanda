package cart_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/shared"
)

func TestNewItem_Valid(t *testing.T) {
	price := shared.MustNewMoney(1500, "EUR")
	item, err := cart.NewItem("var-1", 2, price)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.VariantID != "var-1" {
		t.Errorf("VariantID = %q, want %q", item.VariantID, "var-1")
	}
	if item.Quantity != 2 {
		t.Errorf("Quantity = %d, want 2", item.Quantity)
	}
	if item.UnitPrice.Amount() != 1500 {
		t.Errorf("UnitPrice.Amount = %d, want 1500", item.UnitPrice.Amount())
	}
	if item.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestNewItem_EmptyVariantID(t *testing.T) {
	price := shared.MustNewMoney(1000, "EUR")
	_, err := cart.NewItem("", 1, price)
	if err == nil {
		t.Fatal("expected error for empty variant id")
	}
}

func TestNewItem_ZeroQuantity(t *testing.T) {
	price := shared.MustNewMoney(1000, "EUR")
	_, err := cart.NewItem("var-1", 0, price)
	if err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

func TestNewItem_NegativeQuantity(t *testing.T) {
	price := shared.MustNewMoney(1000, "EUR")
	_, err := cart.NewItem("var-1", -1, price)
	if err == nil {
		t.Fatal("expected error for negative quantity")
	}
}

func TestItem_LineTotal(t *testing.T) {
	price := shared.MustNewMoney(1500, "EUR")
	item, err := cart.NewItem("var-1", 3, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	total, err := item.LineTotal()
	if err != nil {
		t.Fatalf("LineTotal: %v", err)
	}
	if total.Amount() != 4500 {
		t.Errorf("LineTotal = %d, want 4500", total.Amount())
	}
	if total.Currency() != "EUR" {
		t.Errorf("Currency = %q, want %q", total.Currency(), "EUR")
	}
}

func TestItem_LineTotal_Overflow(t *testing.T) {
	price := shared.MustNewMoney(9223372036854775807, "EUR") // math.MaxInt64
	item, err := cart.NewItem("var-1", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	_, err = item.LineTotal()
	if err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestNewItem_NegativePrice(t *testing.T) {
	price := shared.MustNewMoney(-100, "EUR")
	_, err := cart.NewItem("var-1", 1, price)
	if err == nil {
		t.Fatal("expected error for negative price")
	}
}
