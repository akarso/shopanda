package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

// Compile-time check that DownloadableFileRepo implements catalog.DownloadableFileRepository.
var _ catalog.DownloadableFileRepository = (*DownloadableFileRepo)(nil)

// DownloadableFileRepo implements catalog.DownloadableFileRepository using PostgreSQL.
type DownloadableFileRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// downloadableFileQueryer is the subset of *sql.DB and *sql.Tx used by this repo.
type downloadableFileQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewDownloadableFileRepo returns a new DownloadableFileRepo backed by db.
func NewDownloadableFileRepo(db *sql.DB) (*DownloadableFileRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewDownloadableFileRepo: nil *sql.DB")
	}
	return &DownloadableFileRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *DownloadableFileRepo) WithTx(tx *sql.Tx) catalog.DownloadableFileRepository {
	return &DownloadableFileRepo{db: r.db, tx: tx}
}

func (r *DownloadableFileRepo) queryer() downloadableFileQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func mapDownloadableFileExecError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23503":
		switch pgErr.ConstraintName {
		case "downloadable_files_asset_id_fkey":
			return apperror.NotFound("asset not found")
		default:
			return apperror.NotFound("product not found")
		}
	case "23514":
		return apperror.Validation("downloadable file constraint violation")
	case "22P02":
		return apperror.Validation("invalid product id")
	default:
		return err
	}
}

// ListByProductID returns attachments for productID ordered by position,
// then created_at, then asset_id.
func (r *DownloadableFileRepo) ListByProductID(ctx context.Context, productID string) ([]catalog.DownloadableFile, error) {
	if productID == "" {
		return nil, fmt.Errorf("downloadable_file_repo: list: empty product id")
	}
	const q = `SELECT product_id, asset_id, position, label, created_at
		FROM downloadable_files
		WHERE product_id = $1
		ORDER BY position ASC, created_at ASC, asset_id ASC`

	rows, err := r.queryer().QueryContext(ctx, q, productID)
	if err != nil {
		if mapped := mapDownloadableFileExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("downloadable_file_repo: list: %w", err)
	}
	defer rows.Close()

	out := []catalog.DownloadableFile{}
	for rows.Next() {
		var f catalog.DownloadableFile
		if err := rows.Scan(
			&f.ProductID,
			&f.AssetID,
			&f.Position,
			&f.Label,
			&f.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("downloadable_file_repo: list scan: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("downloadable_file_repo: list rows: %w", err)
	}
	return out, nil
}

// CountByProductID returns how many attachment rows belong to the product.
func (r *DownloadableFileRepo) CountByProductID(ctx context.Context, productID string) (int, error) {
	if productID == "" {
		return 0, fmt.Errorf("downloadable_file_repo: count by product: empty product id")
	}
	const q = `SELECT COUNT(*) FROM downloadable_files WHERE product_id = $1`
	var n int
	if err := r.queryer().QueryRowContext(ctx, q, productID).Scan(&n); err != nil {
		if mapped := mapDownloadableFileExecError(err); mapped != err {
			return 0, mapped
		}
		return 0, fmt.Errorf("downloadable_file_repo: count by product: %w", err)
	}
	return n, nil
}

// CountByAssetID returns how many downloadable products reference the asset.
func (r *DownloadableFileRepo) CountByAssetID(ctx context.Context, assetID string) (int, error) {
	if assetID == "" {
		return 0, fmt.Errorf("downloadable_file_repo: count by asset: empty asset id")
	}
	const q = `SELECT COUNT(*) FROM downloadable_files WHERE asset_id = $1`
	var n int
	if err := r.queryer().QueryRowContext(ctx, q, assetID).Scan(&n); err != nil {
		if mapped := mapDownloadableFileExecError(err); mapped != err {
			return 0, mapped
		}
		return 0, fmt.Errorf("downloadable_file_repo: count by asset: %w", err)
	}
	return n, nil
}

// Upsert inserts or updates position and label for the composite key.
func (r *DownloadableFileRepo) Upsert(ctx context.Context, f *catalog.DownloadableFile) error {
	if f == nil {
		return apperror.Validation("downloadable file must not be nil")
	}
	if err := f.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO downloadable_files (product_id, asset_id, position, label, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (product_id, asset_id) DO UPDATE
		SET position = EXCLUDED.position, label = EXCLUDED.label`
	_, err := r.queryer().ExecContext(ctx, q, f.ProductID, f.AssetID, f.Position, f.Label)
	if err != nil {
		if mapped := mapDownloadableFileExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("downloadable_file_repo: upsert: %w", err)
	}
	return nil
}

// Remove deletes one product–asset link.
func (r *DownloadableFileRepo) Remove(ctx context.Context, productID, assetID string) error {
	if productID == "" {
		return fmt.Errorf("downloadable_file_repo: remove: empty product id")
	}
	if assetID == "" {
		return fmt.Errorf("downloadable_file_repo: remove: empty asset id")
	}
	const q = `DELETE FROM downloadable_files WHERE product_id = $1 AND asset_id = $2`
	res, err := r.queryer().ExecContext(ctx, q, productID, assetID)
	if err != nil {
		if mapped := mapDownloadableFileExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("downloadable_file_repo: remove: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("downloadable_file_repo: remove rows affected: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("downloadable file not found")
	}
	return nil
}
