package catalog

import "context"

// BundleComponentRepository persists bundle-to-component links.
type BundleComponentRepository interface {
	// ListByBundleID returns components for bundleProductID ordered by created_at,
	// then component_product_id. Always returns a non-nil slice (possibly empty).
	ListByBundleID(ctx context.Context, bundleProductID string) ([]BundleComponent, error)

	// CountByBundleID returns how many component rows belong to the bundle product.
	CountByBundleID(ctx context.Context, bundleProductID string) (int, error)

	// CountByComponentID returns how many bundles reference the product as a component.
	CountByComponentID(ctx context.Context, componentProductID string) (int, error)

	// Upsert inserts or updates quantity and required for the composite key.
	Upsert(ctx context.Context, c *BundleComponent) error

	// Remove deletes one bundle-component link. NotFound when the row did not exist.
	Remove(ctx context.Context, bundleProductID, componentProductID string) error
}

// BundleParentLookup lists parent bundle product IDs that include a given
// component leaf. Narrower than BundleComponentRepository — used by search
// on-save indexing, FPC/product-cache invalidation, and bulk stock reindex
// (PR-1080) when a leaf's status, type, stock, price, or variant set
// changes. Postgres BundleComponentRepo implements this; it is
// intentionally not on BundleComponentRepository so every test stub does
// not need a no-op method.
type BundleParentLookup interface {
	// ListBundleIDsByComponentID returns distinct parent bundle product IDs
	// that reference componentProductID, ordered ascending. Always returns
	// a non-nil slice (possibly empty). Empty componentProductID returns
	// an empty slice without error.
	ListBundleIDsByComponentID(ctx context.Context, componentProductID string) ([]string, error)

	// ListBundleIDsByComponentIDs returns distinct parent bundle product IDs
	// for each requested component, keyed by component product ID. Missing
	// components are omitted. Always returns a non-nil map. Empty or
	// all-blank input returns an empty map without error.
	ListBundleIDsByComponentIDs(ctx context.Context, componentProductIDs []string) (map[string][]string, error)
}
