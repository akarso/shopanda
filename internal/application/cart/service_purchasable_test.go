package cart_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type purchasableFacts map[string]catalog.VisibilitySnapshot

func (m purchasableFacts) LoadMany(_ context.Context, ids []string) (map[string]catalog.VisibilitySnapshot, error) {
	out := make(map[string]catalog.VisibilitySnapshot, len(ids))
	for _, id := range ids {
		if s, ok := m[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

type erroringFacts struct{}

func (erroringFacts) LoadMany(context.Context, []string) (map[string]catalog.VisibilitySnapshot, error) {
	return nil, errors.New("facts unavailable")
}

func outOfStockSnap() catalog.VisibilitySnapshot {
	s := sellableSnap(catalog.DefaultVisibilityAxes())
	s.Inputs.QuantityPositive = false
	return s
}

func TestService_AddItem_FactsError_NotValidation(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(erroringFacts{}, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if err == nil {
		t.Fatal("expected facts error to surface")
	}
	if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("err = %v, want internal (not validation/not_found)", err)
	}
}

func TestService_HiddenAxis_DecreaseAndRemoveStillAllowed(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}
	facts := purchasableFacts{"prod-1": sellableSnap(catalog.DefaultVisibilityAxes())}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})
	ctx := context.Background()

	c, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "", "var-1", 3, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	modes := catalog.DefaultVisibilityAxes()
	modes.Purchasable = catalog.VisibilityModeHidden
	facts["prod-1"] = sellableSnap(modes)

	if _, err := svc.UpdateItemQuantity(ctx, c.ID, "", "var-1", 1); err != nil {
		t.Fatalf("UpdateItemQuantity decrease on hidden product: %v", err)
	}
	got, err := svc.RemoveItem(ctx, c.ID, "", "var-1")
	if err != nil {
		t.Fatalf("RemoveItem on hidden product: %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(got.Items))
	}
}

func TestService_ClaimGuestCart_MergeDropsOutOfStockLine(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-1")
	seedActiveLine(t, cat, "var-2", "prod-2")
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	facts := purchasableFacts{
		"prod-1": sellableSnap(catalog.DefaultVisibilityAxes()),
		"prod-2": sellableSnap(catalog.DefaultVisibilityAxes()),
	}
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})
	ctx := context.Background()

	customer, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}
	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	for _, vid := range []string{"var-1", "var-2"} {
		if _, err := svc.AddItem(ctx, guest.ID, "", vid, 1, cartApp.AddItemOptions{}); err != nil {
			t.Fatalf("AddItem %s: %v", vid, err)
		}
	}
	facts["prod-2"] = outOfStockSnap()

	merged, err := svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if err != nil {
		t.Fatalf("ClaimGuestCart: %v", err)
	}
	if merged.ID != customer.ID {
		t.Fatalf("merged cart = %q, want %q", merged.ID, customer.ID)
	}
	if len(merged.Items) != 1 || merged.Items[0].VariantID != "var-1" {
		t.Fatalf("items = %+v, want only var-1", merged.Items)
	}
}

func sellableSnap(modes catalog.VisibilityAxes) catalog.VisibilitySnapshot {
	return catalog.VisibilitySnapshot{
		Status: catalog.StatusActive,
		Modes:  modes,
		Inputs: catalog.VisibilityInputs{
			QuantityPositive:   true,
			HasPrice:           true,
			HasStoreAssignment: true,
		},
	}
}

func TestService_AddItem_RejectsNonPurchasableAxis(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}
	modes := catalog.DefaultVisibilityAxes()
	modes.Purchasable = catalog.VisibilityModeHidden
	facts := purchasableFacts{"prod-1": sellableSnap(modes)}

	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if err == nil {
		t.Fatal("expected AddItem to reject non-purchasable product")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Fatalf("err = %v, want validation", err)
	}
	if !strings.Contains(err.Error(), "not currently purchasable") {
		t.Fatalf("err = %q, want not currently purchasable", err.Error())
	}
	if strings.Contains(err.Error(), "not available for purchase") {
		t.Fatalf("err = %q, should be purchasable-axis message not status", err.Error())
	}
}

func TestService_AddItem_AllowsPurchasableAxis(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}
	facts := purchasableFacts{"prod-1": sellableSnap(catalog.DefaultVisibilityAxes())}

	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	got, err := svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(got.Items))
	}
}

func TestService_AddItem_MissingVisibilitySnapshot_FailClosed(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}
	facts := purchasableFacts{} // active product, but no snapshot → deny

	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if err == nil {
		t.Fatal("expected fail-closed when snapshot missing")
	}
	if !strings.Contains(err.Error(), "not currently purchasable") {
		t.Fatalf("err = %q", err.Error())
	}
}

func TestService_AddItem_NilFacts_StatusOnly(t *testing.T) {
	// Axis gate off (enforce=false): active product still sells without facts.
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}

	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(nil, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem with nil facts: %v", err)
	}
}

func TestService_UpdateItemQuantity_RejectsNonPurchasableOnIncrease(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}
	modes := catalog.DefaultVisibilityAxes()
	facts := purchasableFacts{"prod-1": sellableSnap(modes)}

	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	modes.Purchasable = catalog.VisibilityModeHidden
	facts["prod-1"] = sellableSnap(modes)

	_, err = svc.UpdateItemQuantity(context.Background(), c.ID, "", "var-1", 2)
	if err == nil {
		t.Fatal("expected UpdateItemQuantity to reject non-purchasable on increase")
	}
	if !strings.Contains(err.Error(), "not currently purchasable") {
		t.Fatalf("err = %q", err.Error())
	}
}

func TestService_AddItem_PurchasableBeforeExtensions(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}
	modes := catalog.DefaultVisibilityAxes()
	modes.Purchasable = catalog.VisibilityModeHidden
	facts := purchasableFacts{"prod-1": sellableSnap(modes)}

	// extensions writer nil + non-empty Extensions: if gate ran after extension
	// validation, error would be "extension values are not supported".
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.x", Value: "y"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "extension values are not supported") {
		t.Fatal("purchasable gate must run before extension ValidateBatch")
	}
	if !strings.Contains(err.Error(), "not currently purchasable") {
		t.Fatalf("err = %q, want not currently purchasable", err.Error())
	}
}
