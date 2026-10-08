package checkout_test

import (
	"context"
	"database/sql"

	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/shipping"
)

type nilProductRepo047 struct{}

func (r *nilProductRepo047) FindByID(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (r *nilProductRepo047) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (r *nilProductRepo047) List(context.Context, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (r *nilProductRepo047) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (r *nilProductRepo047) Create(context.Context, *catalog.Product) error { return nil }
func (r *nilProductRepo047) Update(context.Context, *catalog.Product) error { return nil }

type errVariantRepo047 struct {
	err error
}

func (r *errVariantRepo047) FindByID(context.Context, string) (*catalog.Variant, error) {
	return nil, r.err
}
func (r *errVariantRepo047) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (r *errVariantRepo047) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (r *errVariantRepo047) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r *errVariantRepo047) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (r *errVariantRepo047) Create(context.Context, *catalog.Variant) error { return nil }
func (r *errVariantRepo047) Update(context.Context, *catalog.Variant) error { return nil }
func (r *errVariantRepo047) WithTx(*sql.Tx) catalog.VariantRepository       { return r }

// mockProductRepo047 returns products keyed by ID. Missing IDs use defaultType
// (TypeSimple when unset) so existing shipping tests keep physical behavior.
type mockProductRepo047 struct {
	products    map[string]*catalog.Product
	defaultType catalog.Type
}

func (r *mockProductRepo047) typeOrDefault() catalog.Type {
	if r.defaultType == "" {
		return catalog.TypeSimple
	}
	return r.defaultType
}

func (r *mockProductRepo047) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	if r.products != nil {
		if p, ok := r.products[id]; ok {
			return p, nil
		}
	}
	return &catalog.Product{ID: id, Name: "P", Slug: "p", Status: catalog.StatusActive, Type: r.typeOrDefault()}, nil
}
func (r *mockProductRepo047) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (r *mockProductRepo047) List(context.Context, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (r *mockProductRepo047) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (r *mockProductRepo047) Create(context.Context, *catalog.Product) error { return nil }
func (r *mockProductRepo047) Update(context.Context, *catalog.Product) error { return nil }

// mockVariantForShipping047 synthesises a variant → product mapping:
// variant ID "v1" → product ID "prod-v1".
type mockVariantForShipping047 struct {
	variants map[string]*catalog.Variant
}

func (r *mockVariantForShipping047) FindByID(_ context.Context, vid string) (*catalog.Variant, error) {
	if r.variants != nil {
		if v, ok := r.variants[vid]; ok {
			return v, nil
		}
	}
	return &catalog.Variant{ID: vid, ProductID: "prod-" + vid, SKU: "sku-" + vid}, nil
}
func (r *mockVariantForShipping047) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (r *mockVariantForShipping047) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (r *mockVariantForShipping047) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r *mockVariantForShipping047) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (r *mockVariantForShipping047) Create(context.Context, *catalog.Variant) error { return nil }
func (r *mockVariantForShipping047) Update(context.Context, *catalog.Variant) error { return nil }
func (r *mockVariantForShipping047) WithTx(*sql.Tx) catalog.VariantRepository       { return r }

func newSelectShippingStep047(
	provider shipping.Provider,
	shipments shipping.ShipmentRepository,
) *checkout.SelectShippingStep {
	return checkout.NewSelectShippingStep(
		shippingRegistryWith(provider),
		shipments,
	)
}

func setShippingRequired047(cctx *checkout.Context, needs bool) {
	cctx.SetMeta(checkout.ShippingRequiredMetaKey, needs)
}
