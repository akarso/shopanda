package cart

import (
	"context"
	"math"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
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

// EmptyBundleComponents is a bundle-component repository with no rows.
// Cart tests and demo plugins pass it to NewService, which requires a non-nil
// component source even when the scenario never prices a dynamic bundle.
func EmptyBundleComponents() catalog.BundleComponentRepository {
	return emptyBundleComponents{}
}

type emptyBundleComponents struct{}

func (emptyBundleComponents) ListByBundleID(context.Context, string) ([]catalog.BundleComponent, error) {
	return nil, nil
}

func (emptyBundleComponents) CountByBundleID(context.Context, string) (int, error) { return 0, nil }

func (emptyBundleComponents) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}

func (emptyBundleComponents) Upsert(context.Context, *catalog.BundleComponent) error { return nil }

func (emptyBundleComponents) Remove(context.Context, string, string) error { return nil }

// PermissiveStock reports a very large on-hand quantity for every variant.
// Cart tests and demo plugins pass it to NewService, which requires a non-nil
// stock repository even when the scenario does not model inventory.
func PermissiveStock() inventory.StockRepository {
	return permissiveStock{}
}

type permissiveStock struct{}

func (permissiveStock) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	return inventory.StockEntry{VariantID: variantID, Quantity: math.MaxInt32}, nil
}

func (permissiveStock) SetStock(context.Context, *inventory.StockEntry) error { return nil }

func (permissiveStock) SetStocks(context.Context, []inventory.StockEntry) error { return nil }

func (permissiveStock) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}

func (permissiveStock) ListInventory(context.Context, int, int, string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}

func (permissiveStock) GetInventoryItem(context.Context, string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}
