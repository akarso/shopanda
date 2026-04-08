package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/media"
)

// Compile-time check.
var _ media.AssetRepository = (*AssetRepo)(nil)

// AssetRepo implements media.AssetRepository using PostgreSQL.
type AssetRepo struct {
	db *sql.DB
}

// NewAssetRepo returns a new AssetRepo backed by db.
func NewAssetRepo(db *sql.DB) *AssetRepo {
	return &AssetRepo{db: db}
}

// Save persists a new asset.
func (r *AssetRepo) Save(ctx context.Context, a *media.Asset) error {
	metaJSON, err := json.Marshal(a.Meta)
	if err != nil {
		return fmt.Errorf("asset_repo: marshal meta: %w", err)
	}

	const q = `INSERT INTO assets (id, path, filename, mime_type, size, meta, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err = r.db.ExecContext(ctx, q,
		a.ID, a.Path, a.Filename, a.MimeType, a.Size, metaJSON, a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("asset_repo: save: %w", err)
	}
	return nil
}

// FindByID returns an asset by its ID.
// Returns (nil, nil) when the asset does not exist.
func (r *AssetRepo) FindByID(ctx context.Context, id string) (*media.Asset, error) {
	const q = `SELECT id, path, filename, mime_type, size, meta, created_at
		FROM assets WHERE id = $1`

	var a media.Asset
	var metaJSON []byte
	err := r.db.QueryRowContext(ctx, q, id).Scan(
		&a.ID, &a.Path, &a.Filename, &a.MimeType, &a.Size, &metaJSON, &a.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("asset_repo: find by id: %w", err)
	}
	if err := json.Unmarshal(metaJSON, &a.Meta); err != nil {
		return nil, fmt.Errorf("asset_repo: unmarshal meta: %w", err)
	}
	return &a, nil
}
