package download

import (
	"context"
	"errors"
)

// ErrCountConflict is returned when UpdateCount matches no row because
// download_count is no longer the value the caller loaded (concurrent
// RecordDownload). The in-memory increment must not be treated as saved.
var ErrCountConflict = errors.New("download grant: download_count conflict")

// Repository persists DownloadGrant rows. Transaction binding is not part
// of this port. WithTx is implemented on the postgres adapter only.
type Repository interface {
	// Insert writes a new grant. A duplicate (order_item_id, asset_id) is a
	// conflict (idempotent order.paid issuance maps this to "already issued").
	Insert(ctx context.Context, g *DownloadGrant) error

	// Update persists expiry / limit / customer_id (extend, revoke).
	// Does not write download_count — use UpdateCount / ResetCount so a
	// concurrent RecordDownload is not clobbered.
	Update(ctx context.Context, g *DownloadGrant) error

	// UpdateCount persists DownloadCount only while the row still has
	// expectedCount (the value before RecordDownload) and is still live
	// (expires_at > now). Zero rows is ErrCountConflict.
	UpdateCount(ctx context.Context, id string, newCount, expectedCount int) error

	// FindByID returns the grant, or (nil, nil) when missing.
	FindByID(ctx context.Context, id string) (*DownloadGrant, error)

	// FindByOrderItemAndAsset returns the grant for that pair, or (nil, nil).
	FindByOrderItemAndAsset(ctx context.Context, orderItemID, assetID string) (*DownloadGrant, error)

	// ListByCustomerID returns grants for customerID, newest first.
	// Always returns a non-nil slice. Callers must reject empty customerID
	// at the application layer (list guests by order item instead).
	ListByCustomerID(ctx context.Context, customerID string) ([]DownloadGrant, error)

	// ListByOrderItemID returns grants for orderItemID, newest first.
	// Always returns a non-nil slice.
	ListByOrderItemID(ctx context.Context, orderItemID string) ([]DownloadGrant, error)

	// CountByAssetID returns how many live grants (expires_at > now)
	// reference the asset (media delete guard). Expired/revoked rows are
	// excluded so they do not pin blobs forever. Always >= 0.
	CountByAssetID(ctx context.Context, assetID string) (int, error)

	// DeleteExpiredOrRevokedByAssetID removes non-live grant rows for the
	// asset so ON DELETE RESTRICT does not block media.Delete after the
	// live-only count check. Returns how many rows were deleted.
	DeleteExpiredOrRevokedByAssetID(ctx context.Context, assetID string) (int, error)
}

// SettingsRepository persists optional per-product grant overrides.
type SettingsRepository interface {
	// FindByProductID returns settings, or (nil, nil) when no override row.
	FindByProductID(ctx context.Context, productID string) (*GrantSettings, error)

	// Upsert inserts or replaces the override row for the product.
	Upsert(ctx context.Context, s *GrantSettings) error

	// Remove deletes the override row. NotFound when missing.
	Remove(ctx context.Context, productID string) error
}
