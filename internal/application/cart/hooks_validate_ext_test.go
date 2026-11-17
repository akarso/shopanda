package cart_test

import (
	"context"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestService_CartValidate_PluginOnlyExtensionVisibleOnlyToOwner(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.RegisterOwned(domainext.FieldDef{
		Code: "acme.design_id", Label: "Design", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Access: domainext.Access{PluginOnlyWrite: true},
	}, "acme"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.note", Label: "Note", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Visibility: domainext.VisibilityPublic,
	}); err != nil {
		t.Fatal(err)
	}
	repo := newMemCartExtensionValueRepo()
	values := extensionapp.NewValueService(reg, repo)
	hookReg := hooks.NewRegistry(nil)

	var ownerSaw interface{}
	var ownerExtCount int
	var otherExtCount int
	var otherPublicSaw interface{}
	var pendingOwnerSaw interface{}
	var pendingOtherPublic interface{}
	var pendingOtherPluginOnly int
	_ = hookReg.Register(hooks.HookCartValidate, 100, "acme", func(hctx *hooks.Context) error {
		if raw, ok := hctx.Get("pending_line"); ok && raw != nil {
			pending, ok := raw.(*extapi.PendingCartLine)
			if ok && pending != nil {
				for _, e := range pending.Extensions {
					if e.Code == "acme.design_id" {
						pendingOwnerSaw = e.Value
					}
				}
			}
		}
		raw, _ := hctx.Get("cart")
		view, ok := raw.(*extapi.CartView)
		if !ok || view == nil || len(view.Items) != 1 {
			return nil
		}
		ownerExtCount = len(view.Items[0].Extensions)
		for _, e := range view.Items[0].Extensions {
			if e.Code == "acme.design_id" {
				ownerSaw = e.Value
			}
		}
		return nil
	})
	_ = hookReg.Register(hooks.HookCartValidate, 200, "other", func(hctx *hooks.Context) error {
		if raw, ok := hctx.Get("pending_line"); ok && raw != nil {
			pending, ok := raw.(*extapi.PendingCartLine)
			if ok && pending != nil {
				for _, e := range pending.Extensions {
					if e.Code == "acme.note" {
						pendingOtherPublic = e.Value
					}
					if e.Code == "acme.design_id" {
						pendingOtherPluginOnly++
					}
				}
			}
		}
		raw, _ := hctx.Get("cart")
		view, ok := raw.(*extapi.CartView)
		if !ok || view == nil || len(view.Items) != 1 {
			return nil
		}
		otherExtCount = len(view.Items[0].Extensions)
		for _, e := range view.Items[0].Extensions {
			if e.Code == "acme.note" {
				otherPublicSaw = e.Value
			}
		}
		return nil
	})

	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), values, hookReg)
	ctx := extensionapp.WithPluginWrite(context.Background(), "acme", "acme.design_id")
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{
			{FieldCode: "acme.design_id", Value: "design-9"},
			{FieldCode: "acme.note", Value: "hello"},
		},
	}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if pendingOwnerSaw != "design-9" {
		t.Fatalf("owner pending_line design = %v, want design-9", pendingOwnerSaw)
	}
	if pendingOtherPublic != "hello" {
		t.Fatalf("other pending public note = %v, want hello", pendingOtherPublic)
	}
	if pendingOtherPluginOnly != 0 {
		t.Fatalf("other pending plugin-only = %d, want omitted", pendingOtherPluginOnly)
	}
	if otherPublicSaw != "hello" {
		t.Fatalf("other cart public note = %v, want hello (overlay)", otherPublicSaw)
	}

	got, err := carts.FindByID(ctx, c.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID: %v %+v", err, got)
	}
	pendingOwnerSaw = nil
	pendingOtherPublic = nil
	if _, err := svc.ValidationIssues(ctx, got); err != nil {
		t.Fatalf("ValidationIssues: %v", err)
	}
	if pendingOwnerSaw != nil || pendingOtherPublic != nil {
		t.Fatal("pending_line must be unset outside AddItem")
	}
	if ownerSaw != "design-9" {
		t.Fatalf("owner stored value=%v, want design-9", ownerSaw)
	}
	if otherExtCount != 1 || otherPublicSaw != "hello" {
		t.Fatalf("other stored public count=%d note=%v", otherExtCount, otherPublicSaw)
	}
	_ = ownerExtCount
}

func TestService_CartValidate_QuantityOnlyAddHasNoPendingLine(t *testing.T) {
	hookReg := hooks.NewRegistry(nil)
	var sawPending bool
	_ = hookReg.Register(hooks.HookCartValidate, 100, "watcher", func(hctx *hooks.Context) error {
		if _, ok := hctx.Get("pending_line"); ok {
			sawPending = true
		}
		return nil
	})
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, hookReg)
	c, err := svc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(context.Background(), c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatal(err)
	}
	if sawPending {
		t.Fatal("quantity-only AddItem must not set pending_line")
	}
	sawPending = false
	if _, err := svc.UpdateItemQuantity(context.Background(), c.ID, "cust-1", "var-1", 2); err != nil {
		t.Fatal(err)
	}
	if sawPending {
		t.Fatal("UpdateItemQuantity must not set pending_line")
	}
}

func TestService_CartValidate_PendingOverlaysStoredEmpty(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.RegisterOwned(domainext.FieldDef{
		Code: "acme.design_id", Label: "Design", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Access: domainext.Access{PluginOnlyWrite: true},
	}, "acme"); err != nil {
		t.Fatal(err)
	}
	repo := newMemCartExtensionValueRepo()
	values := extensionapp.NewValueService(reg, repo)
	hookReg := hooks.NewRegistry(nil)
	var saw string
	_ = hookReg.Register(hooks.HookCartValidate, 100, "acme", func(hctx *hooks.Context) error {
		raw, _ := hctx.Get("cart")
		view, ok := raw.(*extapi.CartView)
		if !ok || view == nil || len(view.Items) != 1 {
			return nil
		}
		for _, e := range view.Items[0].Extensions {
			if e.Code == "acme.design_id" {
				if s, ok := e.Value.(string); ok {
					saw = s
				}
			}
		}
		return nil
	})
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), values, hookReg)
	ctx := extensionapp.WithPluginWrite(context.Background(), "acme", "acme.design_id")
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpsertLineExtension(ctx, c.ID, "cust-1", "var-1", []domainext.ValueInput{
		{FieldCode: "acme.design_id", Value: ""},
	}, "seed"); err != nil {
		t.Fatal(err)
	}
	saw = ""
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.design_id", Value: "design-fixed"}},
	}); err != nil {
		t.Fatalf("AddItem overlay: %v", err)
	}
	if saw != "design-fixed" {
		t.Fatalf("effective line design = %q, want design-fixed (pending overlays stored empty)", saw)
	}
}
