package cart

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// PermissiveCatalogRepos returns variant and product repositories that treat
// every variant as belonging to an active product. Used by unit tests and demo
// plugins that do not model catalog lifecycle.
func PermissiveCatalogRepos() (catalog.VariantRepository, catalog.ProductRepository) {
	return permissiveVariantRepo{}, permissiveProductRepo{}
}

type permissiveVariantRepo struct{}

func (permissiveVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return &catalog.Variant{ID: id, ProductID: "permissive-product"}, nil
}

func (permissiveVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}

func (permissiveVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}

func (permissiveVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}

func (permissiveVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}

func (permissiveVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }

func (permissiveVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

type permissiveProductRepo struct{}

func (permissiveProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return &catalog.Product{ID: id, Status: catalog.StatusActive}, nil
}

func (permissiveProductRepo) FindBySlug(_ context.Context, slug string) (*catalog.Product, error) {
	return &catalog.Product{ID: "permissive-product", Slug: slug, Status: catalog.StatusActive}, nil
}

func (permissiveProductRepo) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}

func (permissiveProductRepo) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}

func (permissiveProductRepo) Create(context.Context, *catalog.Product) error { return nil }

func (permissiveProductRepo) Update(context.Context, *catalog.Product) error { return nil }
