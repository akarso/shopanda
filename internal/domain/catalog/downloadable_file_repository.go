package catalog

import "context"

// DownloadableFileRepository persists downloadable product → media asset links.
type DownloadableFileRepository interface {
	// ListByProductID returns attachments for productID ordered by position,
	// then created_at, then asset_id. Always returns a non-nil slice (possibly empty).
	ListByProductID(ctx context.Context, productID string) ([]DownloadableFile, error)

	// CountByProductID returns how many attachment rows belong to the product.
	CountByProductID(ctx context.Context, productID string) (int, error)

	// CountByAssetID returns how many downloadable products reference the asset.
	CountByAssetID(ctx context.Context, assetID string) (int, error)

	// Upsert inserts or updates position and label for the composite key.
	Upsert(ctx context.Context, f *DownloadableFile) error

	// Remove deletes one product–asset link. NotFound when the row did not exist.
	Remove(ctx context.Context, productID, assetID string) error
}
