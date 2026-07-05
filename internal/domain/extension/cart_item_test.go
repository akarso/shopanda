package extension_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/extension"
)

func TestCartItemTargetID(t *testing.T) {
	got := extension.CartItemTargetID("cart-1", "var-1")
	want := "cart-1:var-1"
	if got != want {
		t.Fatalf("CartItemTargetID = %q, want %q", got, want)
	}
}

func TestCartItemTarget(t *testing.T) {
	target := extension.CartItemTarget("cart-1", "var-1")
	if target.Type != extension.TargetCartItem {
		t.Fatalf("Type = %q, want cart_item", target.Type)
	}
	if target.ID != "cart-1:var-1" {
		t.Fatalf("ID = %q, want cart-1:var-1", target.ID)
	}
}
