package checkout_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type stubCheckoutDownloadableFileCount struct {
	n   int
	err error
}

func (s stubCheckoutDownloadableFileCount) CountByProductID(context.Context, string) (int, error) {
	return s.n, s.err
}

func TestValidateCartStep_MissingDownloadableFile_FactsNilRejects(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
		},
	}}
	step := newValidateCartStep(repo, products).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{n: 0})

	err := step.Execute(context.Background(), linkedParentCartContext(t))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want %q", err, catalog.DownloadableMissingFileNotPurchasableMessage)
	}
}

func TestValidateCartStep_MissingDownloadableFile_EnforceOffStillRejects(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
		},
	}}
	step := newValidateCartStep(repo, products).
		WithVisibilityFacts(nil, catalog.VisibilityOptions{}).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{n: 0})

	err := step.Execute(context.Background(), linkedParentCartContext(t))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want validation despite enforce off", err)
	}
}

func TestValidateCartStep_MissingDownloadableFile_BypassStillRejects(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
		},
	}}
	step := newValidateCartStep(repo, products).
		WithVisibilityFacts(checkoutPurchasableFacts{}, catalog.VisibilityOptions{}).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{n: 0})

	ctx := catalog.WithBypassProductVisibility(context.Background())
	err := step.Execute(ctx, linkedParentCartContext(t))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want validation despite visibility bypass", err)
	}
}

func TestValidateCartStep_DownloadableWithFile_Allows(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
		},
	}}
	step := newValidateCartStep(repo, products).
		WithVisibilityFacts(checkoutPurchasableFacts{"prod-1": checkoutSellableSnap()}, catalog.VisibilityOptions{}).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{n: 1})

	if err := step.Execute(context.Background(), linkedParentCartContext(t)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestValidateCartStep_DownloadableRepoNil_FailsClosed(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
		},
	}}
	step := newValidateCartStep(repo, products)

	err := step.Execute(context.Background(), linkedParentCartContext(t))
	if !apperror.Is(err, apperror.CodeInternal) {
		t.Fatalf("err = %v, want internal fail-closed", err)
	}
}

func TestValidateCartStep_RequiredBundleLeafMissingDownloadableFile(t *testing.T) {
	repo := &mockVariantRepo{variants: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "bundle-1"},
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle-1": {ID: "bundle-1", Status: catalog.StatusActive, Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed},
		"child-1":  {ID: "child-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable},
	}}
	comp, err := catalog.NewBundleComponent("bundle-1", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	step := checkout.NewValidateCartStep(repo, products, linkedParentCheckoutComponents{*comp}, cartApp.PermissiveStock()).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{n: 0})

	err = step.Execute(context.Background(), linkedParentCartContext(t))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want missing-file validation for required leaf", err)
	}
}

func TestValidateCartStep_RequiredBundleLeafDraftMissingDownloadableFile(t *testing.T) {
	repo := &mockVariantRepo{variants: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "bundle-1"},
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle-1": {ID: "bundle-1", Status: catalog.StatusActive, Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed},
		"child-1":  {ID: "child-1", Status: catalog.StatusDraft, Type: catalog.TypeDownloadable},
	}}
	comp, err := catalog.NewBundleComponent("bundle-1", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	step := checkout.NewValidateCartStep(repo, products, linkedParentCheckoutComponents{*comp}, cartApp.PermissiveStock()).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{n: 0})

	err = step.Execute(context.Background(), linkedParentCartContext(t))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want missing-file validation for draft leaf", err)
	}
}

func TestValidateCartStep_DownloadableCountError(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
		},
	}}
	step := newValidateCartStep(repo, products).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{err: errors.New("db down")})

	err := step.Execute(context.Background(), linkedParentCartContext(t))
	if err == nil || !strings.Contains(err.Error(), "count downloadable files") {
		t.Fatalf("err = %v, want count error", err)
	}
}
