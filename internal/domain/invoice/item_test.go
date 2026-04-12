package invoice_test

import (
	"math"
	"testing"

	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/shared"
)

func TestNewItem_Success(t *testing.T) {
	price := shared.MustNewMoney(999, "EUR")
	item, err := invoice.NewItem("v-1", "SKU-001", "Blue Shirt", 3, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	if item.VariantID != "v-1" {
		t.Errorf("VariantID = %q, want v-1", item.VariantID)
	}
	if item.SKU != "SKU-001" {
		t.Errorf("SKU = %q, want SKU-001", item.SKU)
	}
	if item.Name != "Blue Shirt" {
		t.Errorf("Name = %q, want Blue Shirt", item.Name)
	}
	if item.Quantity != 3 {
		t.Errorf("Quantity = %d, want 3", item.Quantity)
	}
}

func TestNewItem_EmptyVariantID(t *testing.T) {
	price := shared.MustNewMoney(100, "EUR")
	_, err := invoice.NewItem("", "SKU", "Shirt", 1, price)
	if err == nil {
		t.Fatal("expected error for empty variant id")
	}
}

func TestNewItem_EmptySKU(t *testing.T) {
	price := shared.MustNewMoney(100, "EUR")
	_, err := invoice.NewItem("v-1", "", "Shirt", 1, price)
	if err == nil {
		t.Fatal("expected error for empty SKU")
	}
}

func TestNewItem_EmptyName(t *testing.T) {
	price := shared.MustNewMoney(100, "EUR")
	_, err := invoice.NewItem("v-1", "SKU", "", 1, price)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestNewItem_ZeroQuantity(t *testing.T) {
	price := shared.MustNewMoney(100, "EUR")
	_, err := invoice.NewItem("v-1", "SKU", "Shirt", 0, price)
	if err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

func TestNewItem_NegativePrice(t *testing.T) {
	price := shared.MustNewMoney(-100, "EUR")
	_, err := invoice.NewItem("v-1", "SKU", "Shirt", 1, price)
	if err == nil {
		t.Fatal("expected error for negative price")
	}
}

func TestItem_LineTotal(t *testing.T) {
	price := shared.MustNewMoney(500, "EUR")
	item, err := invoice.NewItem("v-1", "SKU", "Shirt", 4, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	lt, err := item.LineTotal()
	if err != nil {
		t.Fatalf("LineTotal: %v", err)
	}
	if lt.Amount() != 2000 {
		t.Errorf("LineTotal = %d, want 2000", lt.Amount())
	}
}

func TestItem_LineTotal_Overflow(t *testing.T) {
	price, err := shared.NewMoney(math.MaxInt64, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	item, err := invoice.NewItem("v-1", "SKU", "Shirt", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	_, err = item.LineTotal()
	if err == nil {
		t.Fatal("expected overflow error from LineTotal")
	}
}
