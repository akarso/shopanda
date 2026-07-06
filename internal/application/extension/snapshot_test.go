package extension_test

import (
	"context"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

func TestSnapshotCartItemToOrderItem_CopiesSnapshotFieldsOnly(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
	})
	registerValueField(t, reg, domainext.FieldDef{
		Code:        "acme.cart.note",
		Label:       "Cart note",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	})

	repo := newMemValueRepo()
	values := extensionapp.NewValueService(reg, repo)
	ctx := context.Background()

	cartTarget := domainext.CartItemTarget("cart-1", "var-1")
	msg := "Hello"
	if _, err := values.UpsertBatch(ctx, cartTarget, []domainext.ValueInput{
		{FieldCode: "acme.gift.message", Value: msg},
		{FieldCode: "acme.cart.note", Value: "skip me"},
	}, "cust-1", false); err != nil {
		t.Fatalf("UpsertBatch cart: %v", err)
	}

	if err := values.SnapshotCartItemToOrderItem(ctx, "cart-1", "ord-1", "var-1", "system"); err != nil {
		t.Fatalf("SnapshotCartItemToOrderItem: %v", err)
	}

	orderTarget := domainext.OrderItemTarget("ord-1", "var-1")
	stored, err := repo.ListByTarget(ctx, orderTarget)
	if err != nil {
		t.Fatalf("ListByTarget: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored = %d, want 1", len(stored))
	}
	if stored[0].FieldCode != "acme.gift.message" || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != msg {
		t.Fatalf("stored = %+v", stored)
	}
}
