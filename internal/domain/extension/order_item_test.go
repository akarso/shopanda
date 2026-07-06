package extension_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/extension"
)

func TestOrderItemTargetID(t *testing.T) {
	got := extension.OrderItemTargetID("ord-1", "var-1")
	want := "ord-1:var-1"
	if got != want {
		t.Fatalf("OrderItemTargetID = %q, want %q", got, want)
	}
}

func TestOrderItemTarget(t *testing.T) {
	target := extension.OrderItemTarget("ord-1", "var-1")
	if target.Type != extension.TargetOrderItem {
		t.Fatalf("Type = %q, want order_item", target.Type)
	}
	if target.ID != "ord-1:var-1" {
		t.Fatalf("ID = %q, want ord-1:var-1", target.ID)
	}
}
