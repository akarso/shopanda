package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ download.Repository = (*DownloadGrantRepo)(nil)

const (
	downloadGrantsPK                 = "download_grants_pkey"
	downloadGrantsOrderItemAssetUniq = "download_grants_order_item_asset_unique"
	downloadGrantSelectColumns       = `id, order_item_id, customer_id, asset_id, expires_at, download_limit, download_count, created_at`
)

// DownloadGrantRepo implements download.Repository using PostgreSQL.
type DownloadGrantRepo struct {
	db *sql.DB
	tx *sql.Tx
}

type downloadGrantQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewDownloadGrantRepo returns a new DownloadGrantRepo backed by db.
func NewDownloadGrantRepo(db *sql.DB) (*DownloadGrantRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewDownloadGrantRepo: nil *sql.DB")
	}
	return &DownloadGrantRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *DownloadGrantRepo) WithTx(tx *sql.Tx) download.Repository {
	return &DownloadGrantRepo{db: r.db, tx: tx}
}

func (r *DownloadGrantRepo) queryer() downloadGrantQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func mapDownloadGrantExecError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23505":
		if pgErr.ConstraintName == downloadGrantsOrderItemAssetUniq {
			return apperror.Conflict("download grant already exists for order item and asset")
		}
		if pgErr.ConstraintName == downloadGrantsPK {
			return apperror.Conflict("download grant already exists")
		}
		return apperror.Conflict("download grant conflict")
	case "23503":
		return apperror.NotFound("asset not found")
	case "23514":
		return apperror.Validation("download grant constraint violation")
	case "22P02":
		return apperror.Validation("invalid download grant id")
	default:
		return err
	}
}

// Insert writes a new grant.
func (r *DownloadGrantRepo) Insert(ctx context.Context, g *download.DownloadGrant) error {
	if g == nil {
		return fmt.Errorf("download_grant_repo: insert: grant must not be nil")
	}
	if err := g.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO download_grants
		(id, order_item_id, customer_id, asset_id, expires_at, download_limit, download_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := r.queryer().ExecContext(ctx, q,
		g.ID, g.OrderItemID, g.CustomerID, g.AssetID,
		g.ExpiresAt, g.DownloadLimit, g.DownloadCount, g.CreatedAt,
	)
	if err != nil {
		return mapDownloadGrantExecError(err)
	}
	return nil
}

// Update persists expiry, limit, and count.
func (r *DownloadGrantRepo) Update(ctx context.Context, g *download.DownloadGrant) error {
	if g == nil {
		return fmt.Errorf("download_grant_repo: update: grant must not be nil")
	}
	if err := g.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `UPDATE download_grants
		SET expires_at = $1, download_limit = $2, download_count = $3, customer_id = $4
		WHERE id = $5`
	res, err := r.queryer().ExecContext(ctx, q,
		g.ExpiresAt, g.DownloadLimit, g.DownloadCount, g.CustomerID, g.ID,
	)
	if err != nil {
		return mapDownloadGrantExecError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("download_grant_repo: update rows: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("download grant not found")
	}
	return nil
}

// UpdateCount CAS-updates download_count.
func (r *DownloadGrantRepo) UpdateCount(ctx context.Context, grantID string, newCount, expectedCount int) error {
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return apperror.Validation("download grant id must not be empty")
	}
	const q = `UPDATE download_grants
		SET download_count = $1
		WHERE id = $2 AND download_count = $3`
	res, err := r.queryer().ExecContext(ctx, q, newCount, grantID, expectedCount)
	if err != nil {
		return mapDownloadGrantExecError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("download_grant_repo: update count rows: %w", err)
	}
	if n == 0 {
		return download.ErrCountConflict
	}
	return nil
}

// FindByID returns the grant or (nil, nil).
func (r *DownloadGrantRepo) FindByID(ctx context.Context, grantID string) (*download.DownloadGrant, error) {
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return nil, apperror.Validation("download grant id must not be empty")
	}
	if !id.IsUUID(grantID) {
		return nil, apperror.Validation("invalid download grant id")
	}
	q := `SELECT ` + downloadGrantSelectColumns + ` FROM download_grants WHERE id = $1`
	g, err := scanDownloadGrant(r.queryer().QueryRowContext(ctx, q, grantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		if mapped := mapDownloadGrantExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("download_grant_repo: find: %w", err)
	}
	return g, nil
}

// FindByOrderItemAndAsset returns the grant or (nil, nil).
func (r *DownloadGrantRepo) FindByOrderItemAndAsset(ctx context.Context, orderItemID, assetID string) (*download.DownloadGrant, error) {
	orderItemID = strings.TrimSpace(orderItemID)
	assetID = strings.TrimSpace(assetID)
	if orderItemID == "" || assetID == "" {
		return nil, nil
	}
	q := `SELECT ` + downloadGrantSelectColumns + `
		FROM download_grants WHERE order_item_id = $1 AND asset_id = $2`
	g, err := scanDownloadGrant(r.queryer().QueryRowContext(ctx, q, orderItemID, assetID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("download_grant_repo: find by pair: %w", err)
	}
	return g, nil
}

// ListByCustomerID returns grants newest first.
func (r *DownloadGrantRepo) ListByCustomerID(ctx context.Context, customerID string) ([]download.DownloadGrant, error) {
	q := `SELECT ` + downloadGrantSelectColumns + `
		FROM download_grants WHERE customer_id = $1
		ORDER BY created_at DESC, id DESC`
	return r.list(ctx, q, customerID)
}

// ListByOrderItemID returns grants newest first.
func (r *DownloadGrantRepo) ListByOrderItemID(ctx context.Context, orderItemID string) ([]download.DownloadGrant, error) {
	orderItemID = strings.TrimSpace(orderItemID)
	if orderItemID == "" {
		return []download.DownloadGrant{}, nil
	}
	q := `SELECT ` + downloadGrantSelectColumns + `
		FROM download_grants WHERE order_item_id = $1
		ORDER BY created_at DESC, id DESC`
	return r.list(ctx, q, orderItemID)
}

// CountByAssetID returns how many live grants reference the asset
// (expires_at > now). Expired/revoked rows do not pin blobs forever;
// ON DELETE RESTRICT still applies to live grants.
func (r *DownloadGrantRepo) CountByAssetID(ctx context.Context, assetID string) (int, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return 0, apperror.Validation("asset id must not be empty")
	}
	const q = `SELECT COUNT(*) FROM download_grants WHERE asset_id = $1 AND expires_at > NOW()`
	var n int
	if err := r.queryer().QueryRowContext(ctx, q, assetID).Scan(&n); err != nil {
		return 0, fmt.Errorf("download_grant_repo: count by asset: %w", err)
	}
	return n, nil
}

func (r *DownloadGrantRepo) list(ctx context.Context, q string, arg string) ([]download.DownloadGrant, error) {
	rows, err := r.queryer().QueryContext(ctx, q, arg)
	if err != nil {
		return nil, fmt.Errorf("download_grant_repo: list: %w", err)
	}
	defer rows.Close()
	out := []download.DownloadGrant{}
	for rows.Next() {
		g, err := scanDownloadGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("download_grant_repo: list scan: %w", err)
		}
		out = append(out, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("download_grant_repo: list rows: %w", err)
	}
	return out, nil
}

type downloadGrantScanner interface {
	Scan(dest ...interface{}) error
}

func scanDownloadGrant(row downloadGrantScanner) (*download.DownloadGrant, error) {
	var g download.DownloadGrant
	if err := row.Scan(
		&g.ID, &g.OrderItemID, &g.CustomerID, &g.AssetID,
		&g.ExpiresAt, &g.DownloadLimit, &g.DownloadCount, &g.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &g, nil
}
