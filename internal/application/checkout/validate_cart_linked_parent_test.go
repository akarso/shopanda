package checkout_test

import (
	"context"
	"strings"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func linkedParentCartContext(t *testing.T) *checkout.Context {
	t.Helper()
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1")
	return cctx
}

func TestValidateCartStep_RequiresLinkedParent_FactsNilRejects(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple,
			RequiresLinkedParent: true,
		},
	}}
	step := newValidateCartStep(repo, products)

	err := step.Execute(context.Background(), linkedParentCartContext(t))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.RequiresLinkedParentNotPurchasableMessage) {
		t.Fatalf("err = %v, want %q", err, catalog.RequiresLinkedParentNotPurchasableMessage)
	}
}

func TestValidateCartStep_RequiresLinkedParent_BypassStillRejects(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple,
			RequiresLinkedParent: true,
		},
	}}
	step := newValidateCartStep(repo, products).
		WithVisibilityFacts(checkoutPurchasableFacts{}, catalog.VisibilityOptions{})

	ctx := catalog.WithBypassProductVisibility(context.Background())
	err := step.Execute(ctx, linkedParentCartContext(t))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.RequiresLinkedParentNotPurchasableMessage) {
		t.Fatalf("err = %v, want linked-parent validation despite visibility bypass", err)
	}
}

func TestValidateCartStep_RequiresLinkedParent_ContextAllows(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple,
			RequiresLinkedParent: true,
		},
	}}
	step := newValidateCartStep(repo, products).
		WithVisibilityFacts(checkoutPurchasableFacts{"prod-1": checkoutSellableSnap()}, catalog.VisibilityOptions{})

	ctx := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{Deferred: true})
	if err := step.Execute(ctx, linkedParentCartContext(t)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestValidateCartStep_RequiresLinkedParent_ParentIDAllows(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple,
			RequiresLinkedParent: true,
		},
	}}
	step := newValidateCartStep(repo, products).
		WithVisibilityFacts(checkoutPurchasableFacts{"prod-1": checkoutSellableSnap()}, catalog.VisibilityOptions{})

	ctx := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: "card-1"})
	if err := step.Execute(ctx, linkedParentCartContext(t)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestValidateCartStep_RequiresLinkedParent_WhitespaceParentRejects(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple,
			RequiresLinkedParent: true,
		},
	}}
	step := newValidateCartStep(repo, products)

	ctx := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: "   "})
	err := step.Execute(ctx, linkedParentCartContext(t))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.RequiresLinkedParentNotPurchasableMessage) {
		t.Fatalf("err = %v, want whitespace parent rejected", err)
	}
}

func TestValidateCartStep_RequiresLinkedParent_LineAssignmentAllows(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple,
			RequiresLinkedParent: true,
		},
	}}
	step := newValidateCartStep(repo, products).
		WithVisibilityFacts(checkoutPurchasableFacts{"prod-1": checkoutSellableSnap()}, catalog.VisibilityOptions{})

	cctx := linkedParentCartContext(t)
	cctx.Cart.Items[0].AssignmentDeferred = true
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute with line deferred: %v", err)
	}
}

type linkedParentCheckoutComponents []catalog.BundleComponent

func (c linkedParentCheckoutComponents) ListByBundleID(context.Context, string) ([]catalog.BundleComponent, error) {
	return append([]catalog.BundleComponent(nil), c...), nil
}
func (linkedParentCheckoutComponents) CountByBundleID(context.Context, string) (int, error) {
	return 0, nil
}
func (linkedParentCheckoutComponents) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (linkedParentCheckoutComponents) Upsert(context.Context, *catalog.BundleComponent) error {
	return nil
}
func (linkedParentCheckoutComponents) Remove(context.Context, string, string) error { return nil }

func TestValidateCartStep_RequiredBundleLeafRequiresLinkedParent(t *testing.T) {
	repo := &mockVariantRepo{variants: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "bundle-1"},
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle-1": {ID: "bundle-1", Status: catalog.StatusActive, Type: catalog.TypeBundle},
		"child-1":  {ID: "child-1", Status: catalog.StatusActive, Type: catalog.TypeSimple, RequiresLinkedParent: true},
	}}
	comp, err := catalog.NewBundleComponent("bundle-1", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	step := checkout.NewValidateCartStep(repo, products, linkedParentCheckoutComponents{*comp}, cartApp.PermissiveStock())

	err = step.Execute(context.Background(), linkedParentCartContext(t))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.RequiresLinkedParentNotPurchasableMessage) {
		t.Fatalf("err = %v, want linked-parent validation for required leaf", err)
	}
}

func checkoutFlaggedSnap() catalog.VisibilitySnapshot {
	snap := checkoutSellableSnap()
	snap.RequiresLinkedParent = true
	return snap
}

func TestValidateCartStep_StampedLine_FlaggedSnapshot_EmptyRequest(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple,
			RequiresLinkedParent: true,
		},
	}}
	step := newValidateCartStep(repo, products).
		WithVisibilityFacts(checkoutPurchasableFacts{"prod-1": checkoutFlaggedSnap()}, catalog.VisibilityOptions{})

	cctx := linkedParentCartContext(t)
	cctx.Cart.Items[0].AssignmentParentProductID = "card-1"
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute stamped line, flagged snapshot, empty request: %v", err)
	}
}

func TestValidateCartStep_FlaggedSnapshot_AssignmentNotSharedAcrossLines(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1", "v2")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple},
	}}
	step := newValidateCartStep(repo, products).
		WithVisibilityFacts(checkoutPurchasableFacts{"prod-1": checkoutFlaggedSnap()}, catalog.VisibilityOptions{})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1", "v2")
	cctx.Cart.Items[0].AssignmentDeferred = true

	err := step.Execute(context.Background(), cctx)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), "variant v2") {
		t.Fatalf("err = %v, want v2 rejected without v1's assignment", err)
	}
}

func TestValidateCartStep_DynamicBundleLeaf_StampedLine_EmptyRequest(t *testing.T) {
	child, err := catalog.NewVariant("child-v", "child-1", "CHILD")
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	repo := assignmentVariantRepo{byID: map[string]*catalog.Variant{
		"v1":      {ID: "v1", ProductID: "bundle-1"},
		"child-v": &child,
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle-1": {ID: "bundle-1", Status: catalog.StatusActive, Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic},
		"child-1":  {ID: "child-1", Status: catalog.StatusActive, Type: catalog.TypeSimple, RequiresLinkedParent: true},
	}}
	comp, err := catalog.NewBundleComponent("bundle-1", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	step := checkout.NewValidateCartStep(repo, products, linkedParentCheckoutComponents{*comp}, cartApp.PermissiveStock())

	cctx := linkedParentCartContext(t)
	cctx.Cart.Items[0].AssignmentDeferred = true
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute dynamic bundle stamped line: %v", err)
	}
}

type assignmentVariantRepo struct {
	byID map[string]*catalog.Variant
}

func (r assignmentVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return r.byID[id], nil
}
func (assignmentVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (assignmentVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (assignmentVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r assignmentVariantRepo) ListByProductIDs(_ context.Context, ids []string, _ int) (map[string][]catalog.Variant, error) {
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
func (assignmentVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (assignmentVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }
