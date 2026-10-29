package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxAssetListLimit = 100

// Compile-time check.
var _ media.AssetRepository = (*AssetRepo)(nil)

// AssetRepo implements media.AssetRepository using PostgreSQL.
type AssetRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// assetQueryer is the subset of *sql.DB and *sql.Tx used by this repo.
type assetQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewAssetRepo returns a new AssetRepo backed by db.
func NewAssetRepo(db *sql.DB) (*AssetRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewAssetRepo: nil *sql.DB")
	}
	return &AssetRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *AssetRepo) WithTx(tx *sql.Tx) media.AssetRepository {
	return &AssetRepo{db: r.db, tx: tx}
}

func (r *AssetRepo) queryer() assetQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

// LockForUpdate locks the asset row (SELECT FOR UPDATE) and returns the locked
// row, or NotFound when missing. The repo must be bound to a transaction via
// WithTx (same as ProductRepo.LockForUpdate). Used by DownloadableFileService.Attach
// in the same transaction as the downloadable_files upsert (PR-1074/PR-1077).
func (r *AssetRepo) LockForUpdate(ctx context.Context, id string) (*media.Asset, error) {
	if r.tx == nil {
		return nil, fmt.Errorf("asset_repo: lock for update: transaction required")
	}
	if id == "" {
		return nil, apperror.Validation("asset id must not be empty")
	}
	const q = `SELECT id, path, filename, mime_type, size, meta, thumbnails, created_at
		FROM assets WHERE id = $1 FOR UPDATE`
	a, err := r.scanAsset(r.tx.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.NotFound("asset not found")
	}
	if err != nil {
		return nil, fmt.Errorf("asset_repo: lock for update: %w", err)
	}
	return a, nil
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

	_, err = r.queryer().ExecContext(ctx, q,
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

	a, err := r.scanAsset(r.queryer().QueryRowContext(ctx, q, id))
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
	rows, err := r.queryer().QueryContext(ctx, q, limit, offset)
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
	if id == "" {
		return fmt.Errorf("asset_repo: delete: empty id")
	}
	const q = `DELETE FROM assets WHERE id = $1`
	res, err := r.queryer().ExecContext(ctx, q, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			// Safety net when a join still references the asset
			// (downloadable_files or download_grants ON DELETE RESTRICT).
			// media.Service.Delete counts both before the row delete; this
			// mapping covers a concurrent attach/grant after the counts.
			if strings.Contains(pgErr.ConstraintName, "download_grants") {
				return apperror.Validation("asset is referenced by a download grant")
			}
			return apperror.Validation("asset is still attached to a downloadable product")
		}
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
