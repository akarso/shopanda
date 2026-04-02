package cart_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/shared"
)

func TestNewCart_Valid(t *testing.T) {
	c, err := cart.NewCart("cart-1", "EUR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ID != "cart-1" {
		t.Errorf("ID = %q, want %q", c.ID, "cart-1")
	}
	if c.Currency != "EUR" {
		t.Errorf("Currency = %q, want %q", c.Currency, "EUR")
	}
	if c.Status != cart.CartStatusActive {
		t.Errorf("Status = %q, want %q", c.Status, cart.CartStatusActive)
	}
	if c.CustomerID != "" {
		t.Errorf("CustomerID = %q, want empty", c.CustomerID)
	}
	if c.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestNewCart_EmptyID(t *testing.T) {
	_, err := cart.NewCart("", "EUR")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewCart_InvalidCurrency(t *testing.T) {
	_, err := cart.NewCart("cart-1", "invalid")
	if err == nil {
		t.Fatal("expected error for invalid currency")
	}
}

func TestCart_SetCustomerID(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want %q", c.CustomerID, "cust-1")
	}
}

func TestCart_SetCustomerID_Empty(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	if err := c.SetCustomerID(""); err == nil {
		t.Fatal("expected error for empty customer id")
	}
}

func TestCart_AddItem(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	price := shared.MustNewMoney(1000, "EUR")

	if err := c.AddItem("var-1", 2, price); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if c.ItemCount() != 1 {
		t.Errorf("ItemCount = %d, want 1", c.ItemCount())
	}
	if c.TotalQuantity() != 2 {
		t.Errorf("TotalQuantity = %d, want 2", c.TotalQuantity())
	}
}

func TestCart_AddItem_SameVariantMerges(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	price := shared.MustNewMoney(1000, "EUR")

	if err := c.AddItem("var-1", 2, price); err != nil {
		t.Fatalf("AddItem 1: %v", err)
	}
	if err := c.AddItem("var-1", 3, price); err != nil {
		t.Fatalf("AddItem 2: %v", err)
	}
	if c.ItemCount() != 1 {
		t.Errorf("ItemCount = %d, want 1 (merged)", c.ItemCount())
	}
	if c.TotalQuantity() != 5 {
		t.Errorf("TotalQuantity = %d, want 5", c.TotalQuantity())
	}
}

func TestCart_AddItem_EmptyVariant(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	price := shared.MustNewMoney(1000, "EUR")

	if err := c.AddItem("", 1, price); err == nil {
		t.Fatal("expected error for empty variant id")
	}
}

func TestCart_AddItem_ZeroQuantity(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	price := shared.MustNewMoney(1000, "EUR")

	if err := c.AddItem("var-1", 0, price); err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

func TestCart_AddItem_CurrencyMismatch(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	price := shared.MustNewMoney(1000, "USD")

	if err := c.AddItem("var-1", 1, price); err == nil {
		t.Fatal("expected error for currency mismatch")
	}
}

func TestCart_AddItem_NonActive(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	c.Status = cart.CartStatusCheckedOut
	price := shared.MustNewMoney(1000, "EUR")

	if err := c.AddItem("var-1", 1, price); err == nil {
		t.Fatal("expected error for non-active cart")
	}
}

func TestCart_UpdateItemQuantity(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	price := shared.MustNewMoney(1000, "EUR")
	c.AddItem("var-1", 2, price)

	if err := c.UpdateItemQuantity("var-1", 5); err != nil {
		t.Fatalf("UpdateItemQuantity: %v", err)
	}
	if c.TotalQuantity() != 5 {
		t.Errorf("TotalQuantity = %d, want 5", c.TotalQuantity())
	}
}

func TestCart_UpdateItemQuantity_NotFound(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	if err := c.UpdateItemQuantity("var-1", 1); err == nil {
		t.Fatal("expected error for missing item")
	}
}

func TestCart_UpdateItemQuantity_ZeroQuantity(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	price := shared.MustNewMoney(1000, "EUR")
	c.AddItem("var-1", 2, price)

	if err := c.UpdateItemQuantity("var-1", 0); err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

func TestCart_UpdateItemQuantity_NonActive(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	price := shared.MustNewMoney(1000, "EUR")
	c.AddItem("var-1", 2, price)
	c.Status = cart.CartStatusAbandoned

	if err := c.UpdateItemQuantity("var-1", 3); err == nil {
		t.Fatal("expected error for non-active cart")
	}
}

func TestCart_RemoveItem(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	price := shared.MustNewMoney(1000, "EUR")
	c.AddItem("var-1", 2, price)
	c.AddItem("var-2", 1, price)

	if err := c.RemoveItem("var-1"); err != nil {
		t.Fatalf("RemoveItem: %v", err)
	}
	if c.ItemCount() != 1 {
		t.Errorf("ItemCount = %d, want 1", c.ItemCount())
	}
}

func TestCart_RemoveItem_NotFound(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	if err := c.RemoveItem("var-1"); err == nil {
		t.Fatal("expected error for missing item")
	}
}

func TestCart_RemoveItem_NonActive(t *testing.T) {
	c, _ := cart.NewCart("cart-1", "EUR")
	price := shared.MustNewMoney(1000, "EUR")
	c.AddItem("var-1", 1, price)
	c.Status = cart.CartStatusCheckedOut

	if err := c.RemoveItem("var-1"); err == nil {
		t.Fatal("expected error for non-active cart")
	}
}

func TestCartStatus_IsValid(t *testing.T) {
	tests := []struct {
		s    cart.CartStatus
		want bool
	}{
		{cart.CartStatusActive, true},
		{cart.CartStatusCheckedOut, true},
		{cart.CartStatusAbandoned, true},
		{"unknown", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.s.IsValid(); got != tt.want {
			t.Errorf("CartStatus(%q).IsValid() = %v, want %v", tt.s, got, tt.want)
		}
	}
}
