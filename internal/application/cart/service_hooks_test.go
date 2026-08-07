package cart_test

import (
	"context"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/hooks"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
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

	svc := newCartServiceWithHooks(reg)
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

func TestService_AddItem_InvokesCartAddItemBeforeHook(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var hookRan bool
	if err := reg.Register(hooks.HookCartAddItemBefore, 100, "plugin.test", func(ctx *hooks.Context) error {
		hookRan = true
		if _, ok := ctx.Get("cart"); !ok {
			t.Fatal("expected cart in payload")
		}
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	svc := newCartServiceWithHooks(reg)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 2, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if !hookRan {
		t.Fatal("expected cart.add_item.before hook to run")
	}
}

func TestService_AddItem_BeforeHookBlocksMutation(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	if err := reg.Register(hooks.HookCartAddItemBefore, 100, "plugin.test", func(ctx *hooks.Context) error {
		return context.Canceled
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	svc := newCartServiceWithHooks(reg)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err == nil {
		t.Fatal("expected add item to fail when before hook errors")
	}
	updated, err := svc.GetCart(ctx, c.ID, "cust-1")
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if len(updated.Items) != 0 {
		t.Fatalf("cart items = %d, want 0 when before hook blocks", len(updated.Items))
	}
}

func TestService_UpdateItem_InvokesBeforeHook(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var hookRan bool
	if err := reg.Register(hooks.HookCartUpdateItemBefore, 100, "plugin.test", func(ctx *hooks.Context) error {
		hookRan = true
		qty, ok := ctx.Get("quantity")
		if !ok || qty != 5 {
			t.Fatalf("quantity = %v", qty)
		}
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	svc := newCartServiceWithHooks(reg)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if _, err := svc.UpdateItemQuantity(ctx, c.ID, "cust-1", "var-1", 5); err != nil {
		t.Fatalf("UpdateItemQuantity: %v", err)
	}
	if !hookRan {
		t.Fatal("expected cart.update_item.before hook to run")
	}
}

func TestService_RemoveItem_InvokesAfterHook(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var hookRan bool
	if err := reg.Register(hooks.HookCartRemoveItemAfter, 100, "plugin.test", func(ctx *hooks.Context) error {
		hookRan = true
		if _, ok := ctx.Get("cart"); !ok {
			t.Fatal("expected cart in payload")
		}
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	svc := newCartServiceWithHooks(reg)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if _, err := svc.RemoveItem(ctx, c.ID, "cust-1", "var-1"); err != nil {
		t.Fatalf("RemoveItem: %v", err)
	}
	if !hookRan {
		t.Fatal("expected cart.remove_item.after hook to run")
	}
}

func TestService_AddItem_AfterHookErrorDoesNotFailOperation(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	if err := reg.Register(hooks.HookCartAddItemAfter, 100, "plugin.test", func(ctx *hooks.Context) error {
		return context.Canceled
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	svc := newCartServiceWithHooks(reg)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	updated, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if len(updated.Items) != 1 {
		t.Fatalf("items = %d, want 1 when after hook errors", len(updated.Items))
	}
}

func TestService_RemoveItem_AfterHookErrorDoesNotFailOperation(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	if err := reg.Register(hooks.HookCartRemoveItemAfter, 100, "plugin.test", func(ctx *hooks.Context) error {
		return context.Canceled
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	svc := newCartServiceWithHooks(reg)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	updated, err := svc.RemoveItem(ctx, c.ID, "cust-1", "var-1")
	if err != nil {
		t.Fatalf("RemoveItem: %v", err)
	}
	if len(updated.Items) != 0 {
		t.Fatalf("items = %d, want 0 when after hook errors", len(updated.Items))
	}
}

func TestService_Recalculate_InvokesBeforeHook(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var hookRan bool
	if err := reg.Register(hooks.HookCartRecalculateBefore, 100, "plugin.test", func(ctx *hooks.Context) error {
		hookRan = true
		meta, ok := ctx.Get("pricing_meta")
		if !ok {
			t.Fatal("expected pricing_meta in payload")
		}
		m, ok := meta.(map[string]interface{})
		if !ok {
			t.Fatalf("pricing_meta type = %T", meta)
		}
		m["acme.tier"] = "gold"
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	svc := newCartServiceWithHooksAndPipeline(reg, testPipelineWithGoldTier(prices))
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	updated, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if !hookRan {
		t.Fatal("expected cart.recalculate.before hook to run")
	}
	if updated.Items[0].UnitPrice.Amount() != 1500 {
		t.Fatalf("unit price = %d, want 1500 for acme.tier=gold pricing", updated.Items[0].UnitPrice.Amount())
	}
}

type goldTierPricingStep struct{}

func (s *goldTierPricingStep) Name() string { return "gold_tier" }

func (s *goldTierPricingStep) Apply(_ context.Context, pctx *domainpricing.PricingContext) error {
	tier, _ := pctx.Meta["acme.tier"].(string)
	if tier != "gold" {
		return nil
	}
	uplift := shared.MustNewMoney(500, pctx.Currency)
	for i := range pctx.Items {
		pctx.Items[i].UnitPrice = pctx.Items[i].UnitPrice.Add(uplift)
		total, err := pctx.Items[i].UnitPrice.MulChecked(int64(pctx.Items[i].Quantity))
		if err != nil {
			return err
		}
		pctx.Items[i].Total = total
	}
	return nil
}

func testPipelineWithGoldTier(prices domainpricing.PriceRepository) domainpricing.Pipeline {
	return domainpricing.NewPipeline(
		appPricing.NewBasePriceStep(prices),
		&goldTierPricingStep{},
		domainpricing.NewFinalizeStep(),
	)
}

func newCartServiceWithHooksAndPipeline(reg *hooks.Registry, pipeline domainpricing.Pipeline) *cartApp.Service {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	return cartApp.NewService(carts, prices, nil, nil, pipeline, testLogger(), testBus(), nil, reg)
}

func newCartServiceWithHooks(reg *hooks.Registry) *cartApp.Service {
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	return newCartServiceWithHooksAndPipeline(reg, testPipeline(prices))
}
