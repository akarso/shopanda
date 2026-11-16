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
	repo := newMemCartExtensionValueRepo()
	values := extensionapp.NewValueService(reg, repo)
	hookReg := hooks.NewRegistry(nil)

	var ownerSaw interface{}
	var ownerExtCount int
	var otherExtCount int
	_ = hookReg.Register(hooks.HookCartValidate, 100, "acme", func(hctx *hooks.Context) error {
		raw, _ := hctx.Get("cart")
		view := raw.(*extapi.CartView)
		if len(view.Items) != 1 {
			return nil
		}
		ownerExtCount = len(view.Items[0].Extensions)
		if ownerExtCount == 0 {
			// AddItem runs validate before UpsertBatch — ignore that pass.
			return nil
		}
		ownerSaw = view.Items[0].Extensions[0].Value
		return nil
	})
	_ = hookReg.Register(hooks.HookCartValidate, 200, "other", func(hctx *hooks.Context) error {
		raw, _ := hctx.Get("cart")
		view := raw.(*extapi.CartView)
		if len(view.Items) != 1 {
			return nil
		}
		otherExtCount = len(view.Items[0].Extensions)
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
		Extensions: []domainext.ValueInput{{FieldCode: "acme.design_id", Value: "design-9"}},
	}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	got, err := carts.FindByID(ctx, c.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID: %v %+v", err, got)
	}
	if _, err := svc.ValidationIssues(ctx, got); err != nil {
		t.Fatalf("ValidationIssues: %v", err)
	}
	if ownerExtCount != 1 || ownerSaw != "design-9" {
		t.Fatalf("owner saw value=%v count=%d, want design-9", ownerSaw, ownerExtCount)
	}
	if otherExtCount != 0 {
		t.Fatalf("other saw %d extensions, want omitted", otherExtCount)
	}
}
