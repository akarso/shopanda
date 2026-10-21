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
