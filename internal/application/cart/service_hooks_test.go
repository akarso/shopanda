package cart_test

import (
	"context"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/hooks"
)

func TestService_AddItem_InvokesCartAddItemAfterHook(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var hookRan bool
	if err := reg.Register(hooks.HookCartAddItemAfter, 100, "plugin.test", func(ctx *hooks.Context) error {
		hookRan = true
		v, ok := ctx.Get("variant_id")
		if !ok || v != "var-1" {
			t.Fatalf("variant_id = %v, ok=%v", v, ok)
		}
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus(), nil, reg)

	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if !hookRan {
		t.Fatal("expected cart.add_item.after hook to run")
	}
}
