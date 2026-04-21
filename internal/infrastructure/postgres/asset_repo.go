package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

const maxAssetListLimit = 100

// Compile-time check.
var _ media.AssetRepository = (*AssetRepo)(nil)

// AssetRepo implements media.AssetRepository using PostgreSQL.
type AssetRepo struct {
	db *sql.DB
}

// NewAssetRepo returns a new AssetRepo backed by db.
func NewAssetRepo(db *sql.DB) (*AssetRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewAssetRepo: nil *sql.DB")
	}
	return &AssetRepo{db: db}, nil
}

// Save persists a new asset.
func (r *AssetRepo) Save(ctx context.Context, a *media.Asset) error {
	metaJSON, err := json.Marshal(a.Meta)
	if err != nil {
		return fmt.Errorf("asset_repo: marshal meta: %w", err)
	}
	thumbJSON, err := json.Marshal(a.Thumbnails)
	if err != nil {
		return fmt.Errorf("asset_repo: marshal thumbnails: %w", err)
	}

	const q = `INSERT INTO assets (id, path, filename, mime_type, size, meta, thumbnails, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err = r.db.ExecContext(ctx, q,
		a.ID, a.Path, a.Filename, a.MimeType, a.Size, metaJSON, thumbJSON, a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("asset_repo: save: %w", err)
	}
	return nil
}

// FindByID returns an asset by its ID.
// Returns (nil, nil) when the asset does not exist.
func (r *AssetRepo) FindByID(ctx context.Context, id string) (*media.Asset, error) {
	const q = `SELECT id, path, filename, mime_type, size, meta, thumbnails, created_at
		FROM assets WHERE id = $1`

	a, err := r.scanAsset(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("asset_repo: find by id: %w", err)
	}
	return a, nil
}

// List returns a page of assets ordered by created_at desc.
func (r *AssetRepo) List(ctx context.Context, offset, limit int) ([]media.Asset, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > maxAssetListLimit {
		limit = maxAssetListLimit
	}
	if offset < 0 {
		offset = 0
	}

	const q = `SELECT id, path, filename, mime_type, size, meta, thumbnails, created_at
		FROM assets ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.db.QueryContext(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("asset_repo: list: %w", err)
	}
	defer rows.Close()

	assets := make([]media.Asset, 0)
	for rows.Next() {
		a, scanErr := r.scanAsset(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("asset_repo: list scan: %w", scanErr)
		}
		assets = append(assets, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("asset_repo: list rows: %w", err)
	}
	return assets, nil
}

// Delete removes an asset record by ID.
func (r *AssetRepo) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM assets WHERE id = $1`
	res, err := r.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("asset_repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("asset_repo: delete rows affected: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("asset not found")
	}
	return nil
}

func (r *AssetRepo) scanAsset(scanner interface {
	Scan(dest ...interface{}) error
}) (*media.Asset, error) {
	var a media.Asset
	var metaJSON, thumbJSON []byte
	err := scanner.Scan(&a.ID, &a.Path, &a.Filename, &a.MimeType, &a.Size, &metaJSON, &thumbJSON, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &a.Meta); err != nil {
			return nil, fmt.Errorf("asset_repo: unmarshal meta: %w", err)
		}
	}
	if a.Meta == nil {
		a.Meta = make(map[string]interface{})
	}
	a.Thumbnails = make(map[string]string)
	if len(thumbJSON) > 0 {
		if err := json.Unmarshal(thumbJSON, &a.Thumbnails); err != nil {
			return nil, fmt.Errorf("asset_repo: unmarshal thumbnails: %w", err)
		}
	}
	return &a, nil
}
