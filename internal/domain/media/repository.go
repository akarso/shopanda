package media

import "context"

// AssetRepository defines persistence operations for assets.
type AssetRepository interface {
	// Save persists a new asset.
	Save(ctx context.Context, a *Asset) error

	// FindByID returns an asset by its ID.
	// Returns (nil, nil) when the asset does not exist.
	FindByID(ctx context.Context, id string) (*Asset, error)
}
