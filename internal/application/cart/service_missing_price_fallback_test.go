package cart

import (
	"context"
	"errors"
	"testing"

	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestBundlePriceCustomerError_FallbackReturnsLookupError(t *testing.T) {
	lookupErr := errors.New("variant db down")
	svc := &Service{
		variants: fallbackVariantRepo{err: lookupErr},
		products: fallbackProductRepo{},
	}
	missing := &appPricing.MissingUnitPriceError{VariantID: "v-1", Currency: "EUR"}

	customer, ok := svc.bundlePriceCustomerError(context.Background(), missing)
	if !ok {
		t.Fatal("expected the lookup failure to be returned")
	}
	if !errors.Is(customer, lookupErr) {
		t.Fatalf("err = %v, want the repository error", customer)
	}
	var appErr *apperror.Error
	if errors.As(customer, &appErr) && appErr.Message == appPricing.PriceNotAvailableMessage {
		t.Fatalf("message = %q, want the repository error", appErr.Message)
	}

	svc.variants = fallbackVariantRepo{variant: &catalog.Variant{ID: "v-1", ProductID: "p-1"}}
	svc.products = fallbackProductRepo{err: lookupErr}
	customer, ok = svc.bundlePriceCustomerError(context.Background(), missing)
	if !ok || !errors.Is(customer, lookupErr) {
		t.Fatalf("product lookup err = %v, ok = %v, want the repository error", customer, ok)
	}
	if errors.As(customer, &appErr) && appErr.Message == appPricing.PriceNotAvailableMessage {
		t.Fatalf("message = %q, want the repository error", appErr.Message)
	}
}

type fallbackVariantRepo struct {
	variant *catalog.Variant
	err     error
}

func (r fallbackVariantRepo) FindByID(context.Context, string) (*catalog.Variant, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.variant, nil
}
func (fallbackVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (fallbackVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (fallbackVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (fallbackVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return map[string][]catalog.Variant{}, nil
}
func (fallbackVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (fallbackVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

type fallbackProductRepo struct {
	err error
}

func (r fallbackProductRepo) FindByID(context.Context, string) (*catalog.Product, error) {
	return nil, r.err
}
func (fallbackProductRepo) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (fallbackProductRepo) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (fallbackProductRepo) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (fallbackProductRepo) Create(context.Context, *catalog.Product) error { return nil }
func (fallbackProductRepo) Update(context.Context, *catalog.Product) error { return nil }
