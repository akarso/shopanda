package cart_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type stubDownloadableFileCount struct {
	n   int
	err error
}

func (s stubDownloadableFileCount) CountByProductID(context.Context, string) (int, error) {
	return s.n, s.err
}

func TestService_AddItem_MissingDownloadableFile_FactsNilRejects(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithDownloadableFiles(stubDownloadableFileCount{n: 0})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want validation %q", err, catalog.DownloadableMissingFileNotPurchasableMessage)
	}
}

func TestService_AddItem_MissingDownloadableFile_EnforceOffStillRejects(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
	}}
	// Enforce off: nil facts (axis gate off). Gate must still reject.
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(nil, catalog.VisibilityOptions{}).
		WithDownloadableFiles(stubDownloadableFileCount{n: 0})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want validation despite enforce off", err)
	}
}

func TestService_AddItem_MissingDownloadableFile_BypassStillRejects(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(purchasableFacts{}, catalog.VisibilityOptions{}).
		WithDownloadableFiles(stubDownloadableFileCount{n: 0})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	ctx := catalog.WithBypassProductVisibility(context.Background())
	_, err = svc.AddItem(ctx, c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want validation despite visibility bypass", err)
	}
}

func TestService_AddItem_DownloadableWithFile_AllowsWhenSellable(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
	}}
	facts := purchasableFacts{"prod-1": sellableSnap(catalog.DefaultVisibilityAxes())}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{}).
		WithDownloadableFiles(stubDownloadableFileCount{n: 1})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
}

func TestService_AddItem_DownloadableRepoNil_FailsClosed(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil)

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeInternal) {
		t.Fatalf("err = %v, want internal fail-closed", err)
	}
}

func TestService_AddItem_RequiredBundleLeafMissingDownloadableFile(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("bundle-v", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "bundle-v", ProductID: "bundle-1"}}
	pr := linkedParentProductMap{
		"bundle-1": {ID: "bundle-1", Status: catalog.StatusActive, Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed},
		"child-1":  {ID: "child-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable},
	}
	comp, err := catalog.NewBundleComponent("bundle-1", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, linkedParentComponents{*comp}, cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithDownloadableFiles(stubDownloadableFileCount{n: 0})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "bundle-v", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want missing-file validation for required leaf", err)
	}
}

func TestService_AddItem_RequiredBundleLeafDraftMissingDownloadableFile(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("bundle-v", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "bundle-v", ProductID: "bundle-1"}}
	pr := linkedParentProductMap{
		"bundle-1": {ID: "bundle-1", Status: catalog.StatusActive, Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed},
		"child-1":  {ID: "child-1", Status: catalog.StatusDraft, Type: catalog.TypeDownloadable},
	}
	comp, err := catalog.NewBundleComponent("bundle-1", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, linkedParentComponents{*comp}, cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithDownloadableFiles(stubDownloadableFileCount{n: 0})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "bundle-v", 1, cartApp.AddItemOptions{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want missing-file validation for draft leaf", err)
	}
}

func TestService_AddItem_DownloadableCountError(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-1"}}
	pr := addItemProductStub{product: &catalog.Product{
		ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), nil, nil).
		WithDownloadableFiles(stubDownloadableFileCount{err: errors.New("db down")})

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if err == nil || !strings.Contains(err.Error(), "count downloadable files") {
		t.Fatalf("err = %v, want count error", err)
	}
}
