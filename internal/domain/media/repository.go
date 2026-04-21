package media

import "context"

// AssetRepository defines persistence operations for assets.
type AssetRepository interface {
	// Save persists a new asset.
	Save(ctx context.Context, a *Asset) error

	// FindByID returns an asset by its ID.
	// Returns (nil, nil) when the asset does not exist.
	FindByID(ctx context.Context, id string) (*Asset, error)

	// List returns a page of assets ordered by created_at desc.
	List(ctx context.Context, offset, limit int) ([]Asset, error)

	// Delete removes an asset record by ID.
	Delete(ctx context.Context, id string) error
}
