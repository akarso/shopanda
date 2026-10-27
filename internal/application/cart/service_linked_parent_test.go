package cart_test

import (
	"context"
	"strings"
	"testing"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestService_AddItem_RequiresLinkedParent_FactsNilRejects(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, RequiresLinkedParent: true,
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil)

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.RequiresLinkedParentNotPurchasableMessage) {
		t.Fatalf("err = %v, want validation %q", err, catalog.RequiresLinkedParentNotPurchasableMessage)
	}
}

func TestService_AddItem_RequiresLinkedParent_BypassStillRejects(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, RequiresLinkedParent: true,
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(purchasableFacts{}, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	ctx := catalog.WithBypassProductVisibility(context.Background())
	_, err = svc.AddItem(ctx, c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.RequiresLinkedParentNotPurchasableMessage) {
		t.Fatalf("err = %v, want linked-parent validation despite visibility bypass", err)
	}
}

func TestService_AddItem_RequiresLinkedParent_ContextAllowsWhenSellable(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, RequiresLinkedParent: true,
	}}
	facts := purchasableFacts{"prod-1": sellableSnap(catalog.DefaultVisibilityAxes())}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	ctx := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: " card-1 "})
	got, err := svc.AddItem(ctx, c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem with parent: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(got.Items))
	}

	ctx = catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{Deferred: true})
	c2, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c2.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem deferred: %v", err)
	}
}

func TestService_ClaimGuestCart_DropsRequiresLinkedParentLine(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-1")
	seedActiveLine(t, cat, "var-2", "prod-2")
	cat.setProduct(&catalog.Product{ID: "prod-2", Status: catalog.StatusActive, RequiresLinkedParent: true})

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

	if _, err := svc.CreateCart(ctx, "cust-1", "EUR"); err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}
	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	// Guest add of the linked child needs a context; the claim itself does not.
	addCtx := catalog.WithAssignmentContext(ctx, catalog.AssignmentContext{ParentProductID: "card-1"})
	for _, vid := range []string{"var-1", "var-2"} {
		if _, err := svc.AddItem(addCtx, guest.ID, "", vid, 1, cartApp.AddItemOptions{}); err != nil {
			t.Fatalf("AddItem %s: %v", vid, err)
		}
	}

	merged, err := svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if err != nil {
		t.Fatalf("ClaimGuestCart: %v", err)
	}
	if len(merged.Items) != 2 {
		t.Fatalf("items = %+v, want both lines (stored assignment kept)", merged.Items)
	}
	byID := map[string]bool{}
	for _, item := range merged.Items {
		byID[item.VariantID] = true
		if item.VariantID == "var-2" && !item.HasStoredAssignment() {
			t.Fatal("claimed linked-parent line lost stored assignment")
		}
	}
	if !byID["var-1"] || !byID["var-2"] {
		t.Fatalf("items = %+v, want var-1 and var-2", merged.Items)
	}
}

func TestService_ClaimGuestCart_DropsUnstampedRequiresLinkedParentLine(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-1")
	seedActiveLine(t, cat, "var-2", "prod-2")
	cat.setProduct(&catalog.Product{ID: "prod-2", Status: catalog.StatusActive, RequiresLinkedParent: true})

	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil)
	ctx := context.Background()

	if _, err := svc.CreateCart(ctx, "cust-1", "EUR"); err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}
	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	if _, err := svc.AddItem(ctx, guest.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem var-1: %v", err)
	}
	stored, err := carts.FindByID(ctx, guest.ID)
	if err != nil || stored == nil {
		t.Fatalf("FindByID guest: %v %v", stored, err)
	}
	if err := stored.AddItem("var-2", 1, mustEUR(t, 2000)); err != nil {
		t.Fatalf("inject unstamped line: %v", err)
	}
	if err := carts.Save(ctx, stored); err != nil {
		t.Fatalf("Save: %v", err)
	}

	merged, err := svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if err != nil {
		t.Fatalf("ClaimGuestCart: %v", err)
	}
	if len(merged.Items) != 1 || merged.Items[0].VariantID != "var-1" {
		t.Fatalf("items = %+v, want only var-1", merged.Items)
	}
}

func mustEUR(t *testing.T, amount int64) shared.Money {
	t.Helper()
	m, err := shared.NewMoney(amount, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestService_AddItem_RequiresLinkedParent_StampsLineAndWhitespaceRejects(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, RequiresLinkedParent: true,
	}}
	facts := purchasableFacts{"prod-1": sellableSnap(catalog.DefaultVisibilityAxes())}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	blank := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: "   "})
	_, err = svc.AddItem(blank, c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.RequiresLinkedParentNotPurchasableMessage) {
		t.Fatalf("whitespace parent err = %v", err)
	}

	ctx := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: " card-1 "})
	got, err := svc.AddItem(ctx, c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].AssignmentParent() != "card-1" {
		t.Fatalf("line = %+v, want stamped parent card-1", got.Items)
	}
}

func TestService_UpdateItemQuantity_RequiresLinkedParent_IncreaseRejects(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, RequiresLinkedParent: true,
	}}
	facts := purchasableFacts{"prod-1": sellableSnap(catalog.DefaultVisibilityAxes())}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	ctx := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{Deferred: true})
	if _, err := svc.AddItem(ctx, c.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	stored, err := carts.FindByID(context.Background(), c.ID)
	if err != nil || stored == nil || len(stored.Items) != 1 {
		t.Fatalf("stored = %+v err=%v", stored, err)
	}
	stored.Items[0].AssignmentParentProductID = ""
	stored.Items[0].AssignmentDeferred = false
	if err := carts.Save(context.Background(), stored); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, err = svc.UpdateItemQuantity(context.Background(), c.ID, "", "var-1", 2)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.RequiresLinkedParentNotPurchasableMessage) {
		t.Fatalf("increase err = %v, want linked-parent validation", err)
	}
}

type linkedParentProductMap map[string]*catalog.Product

func (m linkedParentProductMap) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return m[id], nil
}
func (linkedParentProductMap) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (linkedParentProductMap) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (linkedParentProductMap) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (linkedParentProductMap) Create(context.Context, *catalog.Product) error { return nil }
func (linkedParentProductMap) Update(context.Context, *catalog.Product) error { return nil }

type linkedParentComponents []catalog.BundleComponent

func (c linkedParentComponents) ListByBundleID(context.Context, string) ([]catalog.BundleComponent, error) {
	return append([]catalog.BundleComponent(nil), c...), nil
}
func (linkedParentComponents) CountByBundleID(context.Context, string) (int, error) { return 0, nil }
func (linkedParentComponents) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (linkedParentComponents) Upsert(context.Context, *catalog.BundleComponent) error { return nil }
func (linkedParentComponents) Remove(context.Context, string, string) error           { return nil }

func TestService_AddItem_RequiredBundleLeafRequiresLinkedParent(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("bundle-v", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "bundle-v", ProductID: "bundle-1"}}
	pr := linkedParentProductMap{
		"bundle-1": {ID: "bundle-1", Status: catalog.StatusActive, Type: catalog.TypeBundle},
		"child-1":  {ID: "child-1", Status: catalog.StatusActive, Type: catalog.TypeSimple, RequiresLinkedParent: true},
	}
	comp, err := catalog.NewBundleComponent("bundle-1", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, linkedParentComponents{*comp}, cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil)

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "bundle-v", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.RequiresLinkedParentNotPurchasableMessage) {
		t.Fatalf("err = %v, want linked-parent validation for required leaf", err)
	}
}

func flaggedSellableSnap() catalog.VisibilitySnapshot {
	snap := sellableSnap(catalog.DefaultVisibilityAxes())
	snap.RequiresLinkedParent = true
	return snap
}

func TestService_UpdateItemQuantity_StampedLine_FlaggedSnapshot_EmptyRequest(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, RequiresLinkedParent: true,
	}}
	facts := purchasableFacts{"prod-1": flaggedSellableSnap()}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	addCtx := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: "card-1"})
	if _, err := svc.AddItem(addCtx, c.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	got, err := svc.UpdateItemQuantity(context.Background(), c.ID, "", "var-1", 2)
	if err != nil {
		t.Fatalf("increase with stamped line and empty request: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Quantity != 2 || got.Items[0].AssignmentParent() != "card-1" {
		t.Fatalf("line = %+v, want qty 2 parent card-1", got.Items)
	}
}

func TestService_ClaimGuestCart_StampedLine_FlaggedSnapshot_EmptyRequest(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-1")
	seedActiveLine(t, cat, "var-2", "prod-2")
	cat.setProduct(&catalog.Product{ID: "prod-2", Status: catalog.StatusActive, RequiresLinkedParent: true})

	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	flagged := flaggedSellableSnap()
	facts := purchasableFacts{
		"prod-1": sellableSnap(catalog.DefaultVisibilityAxes()),
		"prod-2": flagged,
	}
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})
	ctx := context.Background()

	if _, err := svc.CreateCart(ctx, "cust-1", "EUR"); err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}
	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	addCtx := catalog.WithAssignmentContext(ctx, catalog.AssignmentContext{Deferred: true})
	for _, vid := range []string{"var-1", "var-2"} {
		if _, err := svc.AddItem(addCtx, guest.ID, "", vid, 1, cartApp.AddItemOptions{}); err != nil {
			t.Fatalf("AddItem %s: %v", vid, err)
		}
	}

	merged, err := svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if err != nil {
		t.Fatalf("ClaimGuestCart: %v", err)
	}
	if len(merged.Items) != 2 {
		t.Fatalf("items = %+v, want both lines kept", merged.Items)
	}
	for _, item := range merged.Items {
		if item.VariantID == "var-2" && !item.AssignmentDeferred {
			t.Fatal("claimed linked-parent line lost deferred stamp")
		}
	}
}

func TestService_UpdateItemQuantity_DynamicBundleLeaf_StampedLine_EmptyRequest(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("bundle-v", "EUR", 1000)
	child, err := catalog.NewVariant("child-v", "child-1", "CHILD")
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	vr := linkedParentVariantRepo{byID: map[string]*catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle-1"},
		"child-v":  &child,
	}}
	pr := linkedParentProductMap{
		"bundle-1": {ID: "bundle-1", Status: catalog.StatusActive, Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, PricingMode: catalog.PricingModeFixed},
		"child-1":  {ID: "child-1", Status: catalog.StatusActive, Type: catalog.TypeSimple, RequiresLinkedParent: true},
	}
	comp, err := catalog.NewBundleComponent("bundle-1", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, linkedParentComponents{*comp}, cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil)

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	addCtx := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{Deferred: true})
	if _, err := svc.AddItem(addCtx, c.ID, "", "bundle-v", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	got, err := svc.UpdateItemQuantity(context.Background(), c.ID, "", "bundle-v", 2)
	if err != nil {
		t.Fatalf("increase dynamic bundle with stamped line: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Quantity != 2 || !got.Items[0].AssignmentDeferred {
		t.Fatalf("line = %+v, want qty 2 deferred", got.Items)
	}
}

type linkedParentVariantRepo struct {
	byID map[string]*catalog.Variant
}

func (r linkedParentVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return r.byID[id], nil
}
func (linkedParentVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (linkedParentVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (linkedParentVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r linkedParentVariantRepo) ListByProductIDs(_ context.Context, ids []string, _ int) (map[string][]catalog.Variant, error) {
	out := make(map[string][]catalog.Variant)
	for _, id := range ids {
		for _, v := range r.byID {
			if v != nil && v.ProductID == id {
				out[id] = append(out[id], *v)
			}
		}
	}
	return out, nil
}
func (linkedParentVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (linkedParentVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

func TestService_AddItem_RejectsConflictingLinkedParentAssignment(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil)

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	first := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: "card-a"})
	if _, err := svc.AddItem(first, c.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	second := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: "card-b"})
	_, err = svc.AddItem(second, c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), "different linked parent assignment") {
		t.Fatalf("err = %v, want conflicting assignment", err)
	}
	stored, err := carts.FindByID(context.Background(), c.ID)
	if err != nil || stored == nil || len(stored.Items) != 1 || stored.Items[0].Quantity != 1 || stored.Items[0].AssignmentParent() != "card-a" {
		t.Fatalf("stored = %+v err=%v, want qty 1 parent card-a", stored, err)
	}
}

func TestService_ClaimGuestCart_RejectsConflictingParents(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil)
	ctx := context.Background()

	customer, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}
	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	parentA := catalog.WithAssignmentContext(ctx, catalog.AssignmentContext{ParentProductID: "card-a"})
	parentB := catalog.WithAssignmentContext(ctx, catalog.AssignmentContext{ParentProductID: "card-b"})
	if _, err := svc.AddItem(parentA, customer.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem customer: %v", err)
	}
	if _, err := svc.AddItem(parentB, guest.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem guest: %v", err)
	}

	_, err = svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), "different linked parent assignment") {
		t.Fatalf("err = %v, want conflicting assignment", err)
	}
	stored, err := carts.FindByID(ctx, customer.ID)
	if err != nil || stored == nil || len(stored.Items) != 1 || stored.Items[0].Quantity != 1 || stored.Items[0].AssignmentParent() != "card-a" {
		t.Fatalf("customer = %+v err=%v, want qty 1 parent card-a", stored, err)
	}
}

func TestService_ClaimGuestCart_MergesSameLinkedParent(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-1", Status: catalog.StatusActive}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil)
	ctx := context.Background()

	customer, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}
	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	parent := catalog.WithAssignmentContext(ctx, catalog.AssignmentContext{ParentProductID: "card-a"})
	if _, err := svc.AddItem(parent, customer.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem customer: %v", err)
	}
	if _, err := svc.AddItem(parent, guest.ID, "", "var-1", 2, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem guest: %v", err)
	}
	merged, err := svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if err != nil {
		t.Fatalf("ClaimGuestCart: %v", err)
	}
	if len(merged.Items) != 1 || merged.Items[0].Quantity != 3 || merged.Items[0].AssignmentParent() != "card-a" {
		t.Fatalf("merged = %+v, want qty 3 parent card-a", merged.Items)
	}
}

type cartEligStub struct {
	ok bool
}

func (s cartEligStub) IsEligible(context.Context, string, string) (bool, error) {
	return s.ok, nil
}

type cartAssignRepoStub struct{}

func (cartAssignRepoStub) Insert(context.Context, *assignment.ProductAssignment) error { return nil }
func (cartAssignRepoStub) Update(context.Context, *assignment.ProductAssignment) error { return nil }
func (cartAssignRepoStub) FindByID(context.Context, string) (*assignment.ProductAssignment, error) {
	return nil, nil
}
func (cartAssignRepoStub) FindCurrentByChildID(context.Context, string) (*assignment.ProductAssignment, error) {
	return nil, nil
}
func (cartAssignRepoStub) ListByChildID(context.Context, string) ([]assignment.ProductAssignment, error) {
	return nil, nil
}
func (cartAssignRepoStub) ListByParentID(context.Context, string, assignment.ParentPage) ([]assignment.ProductAssignment, bool, error) {
	return nil, false, nil
}

type cartOrderStub struct{}

func (cartOrderStub) FindByID(context.Context, string) (*order.Order, error) { return nil, nil }
func (cartOrderStub) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}

type cartVariantLookupStub struct{}

func (cartVariantLookupStub) FindByID(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}

type cartPaymentStub struct{}

func (cartPaymentStub) FindByOrderID(context.Context, string) (*payment.Payment, error) {
	return nil, nil
}

func TestService_AddItem_RequiresLinkedParent_IneligibleRejects(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, RequiresLinkedParent: true,
	}}
	repo := cartAssignRepoStub{}
	flow := assignmentapp.NewFlow(assignmentapp.NewService(repo), repo, cartEligStub{ok: false}, cartOrderStub{}, cartVariantLookupStub{}, cartPaymentStub{})
	facts := purchasableFacts{"prod-1": sellableSnap(catalog.DefaultVisibilityAxes())}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{}).
		WithAssignmentFlow(flow)

	c, err := svc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	ctx := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: "card-1"})
	_, err = svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignmentapp.ParentNotEligibleMessage) {
		t.Fatalf("err = %v, want %q", err, assignmentapp.ParentNotEligibleMessage)
	}
}
