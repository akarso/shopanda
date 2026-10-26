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
