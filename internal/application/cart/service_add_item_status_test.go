package cart_test

import (
	"context"
	"errors"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type addItemVariantStub struct {
	variant *catalog.Variant
}

func (s addItemVariantStub) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	if s.variant != nil {
		return s.variant, nil
	}
	return &catalog.Variant{ID: id, ProductID: "prod-1"}, nil
}

func (addItemVariantStub) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}

func (addItemVariantStub) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}

func (addItemVariantStub) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}

func (addItemVariantStub) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}

func (addItemVariantStub) Create(context.Context, *catalog.Variant) error { return nil }

func (addItemVariantStub) Update(context.Context, *catalog.Variant) error { return nil }

type addItemProductStub struct {
	product *catalog.Product
}

func (s addItemProductStub) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	if s.product != nil {
		return s.product, nil
	}
	return &catalog.Product{ID: id, Status: catalog.StatusActive}, nil
}

func (addItemProductStub) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}

func (addItemProductStub) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}

func (addItemProductStub) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}

func (addItemProductStub) Create(context.Context, *catalog.Product) error { return nil }

func (addItemProductStub) Update(context.Context, *catalog.Product) error { return nil }

func TestService_AddItem_RejectsArchivedProduct(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-arch"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-arch", Status: catalog.StatusArchived}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, testPipeline(prices), testLogger(), testBus(), nil, nil)

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if err == nil {
		t.Fatal("AddItem: expected error for archived product")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Fatalf("AddItem err = %v, want validation", err)
	}
}

func TestService_AddItem_RejectsNonActiveProduct(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr := addItemVariantStub{variant: &catalog.Variant{ID: "var-1", ProductID: "prod-draft"}}
	pr := addItemProductStub{product: &catalog.Product{ID: "prod-draft", Status: catalog.StatusDraft}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, testPipeline(prices), testLogger(), testBus(), nil, nil)

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}

	_, err = svc.AddItem(context.Background(), c.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	if err == nil {
		t.Fatal("AddItem: expected error for draft product")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Fatalf("AddItem err = %v, want validation", err)
	}
}
