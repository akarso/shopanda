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

// Insert writes a new grant. ON CONFLICT DO NOTHING maps to Conflict so a
// losing concurrent insert does not abort the surrounding Postgres TX
// (unique violations otherwise leave the TX unusable for reload).
func (r *DownloadGrantRepo) Insert(ctx context.Context, g *download.DownloadGrant) error {
	if g == nil {
		return fmt.Errorf("download_grant_repo: insert: grant must not be nil")
	}
	if err := g.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO download_grants
		(id, order_item_id, customer_id, asset_id, expires_at, download_limit, download_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (order_item_id, asset_id) DO NOTHING`
	res, err := r.queryer().ExecContext(ctx, q,
		g.ID, g.OrderItemID, g.CustomerID, g.AssetID,
		g.ExpiresAt, g.DownloadLimit, g.DownloadCount, g.CreatedAt,
	)
	if err != nil {
		return mapDownloadGrantExecError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("download_grant_repo: insert rows: %w", err)
	}
	if n == 0 {
		return apperror.Conflict("download grant already exists for order item and asset")
	}
	return nil
}

// Update persists expiry, limit, and customer_id. download_count is owned by
// UpdateCount so Extend/Revoke cannot overwrite a concurrent delivery.
func (r *DownloadGrantRepo) Update(ctx context.Context, g *download.DownloadGrant) error {
	if g == nil {
		return fmt.Errorf("download_grant_repo: update: grant must not be nil")
	}
	if err := g.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `UPDATE download_grants
		SET expires_at = $1, download_limit = $2, customer_id = $3
		WHERE id = $4`
	res, err := r.queryer().ExecContext(ctx, q,
		g.ExpiresAt, g.DownloadLimit, g.CustomerID, g.ID,
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

// UpdateCount CAS-updates download_count while the grant is still live.
func (r *DownloadGrantRepo) UpdateCount(ctx context.Context, grantID string, newCount, expectedCount int) error {
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return apperror.Validation("download grant id must not be empty")
	}
	const q = `UPDATE download_grants
		SET download_count = $1
		WHERE id = $2 AND download_count = $3 AND expires_at > NOW()`
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

// ClaimDownload atomically increments download_count for an owned live grant.
func (r *DownloadGrantRepo) ClaimDownload(ctx context.Context, grantID, customerID string) (*download.DownloadGrant, error) {
	grantID = strings.TrimSpace(grantID)
	customerID = strings.TrimSpace(customerID)
	if grantID == "" {
		return nil, apperror.Validation("download grant id must not be empty")
	}
	if customerID == "" {
		return nil, apperror.Validation("customer id must not be empty")
	}
	if !id.IsUUID(grantID) {
		return nil, apperror.Validation("invalid download grant id")
	}
	q := `UPDATE download_grants
		SET download_count = download_count + 1
		WHERE id = $1
		  AND customer_id = $2
		  AND download_count < download_limit
		  AND expires_at > NOW()
		RETURNING ` + downloadGrantSelectColumns
	g, err := scanDownloadGrant(r.queryer().QueryRowContext(ctx, q, grantID, customerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, download.ErrCountConflict
	}
	if err != nil {
		if mapped := mapDownloadGrantExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("download_grant_repo: claim download: %w", err)
	}
	return g, nil
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

// LockForUpdate locks the grant row FOR UPDATE inside the current transaction.
func (r *DownloadGrantRepo) LockForUpdate(ctx context.Context, grantID string) (*download.DownloadGrant, error) {
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return nil, apperror.Validation("download grant id must not be empty")
	}
	if !id.IsUUID(grantID) {
		return nil, apperror.Validation("invalid download grant id")
	}
	if r.tx == nil {
		return nil, fmt.Errorf("download_grant_repo: LockForUpdate requires a transaction")
	}
	q := `SELECT ` + downloadGrantSelectColumns + ` FROM download_grants WHERE id = $1 FOR UPDATE`
	g, err := scanDownloadGrant(r.queryer().QueryRowContext(ctx, q, grantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.NotFound("download grant not found")
	}
	if err != nil {
		if mapped := mapDownloadGrantExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("download_grant_repo: lock: %w", err)
	}
	return g, nil
}

// RetargetAsset moves download_grants from oldAssetID to newAssetID by asset
// id only (no order_items/product join). That covers direct SKUs and bundle
// leaf grants minted under the parent bundle order_item_id (PR-1078).
//
// Conflict rules for UNIQUE (order_item_id, asset_id):
//  1. Delete expired/revoked rows already on newAssetID for the same unit so
//     a live (or expired) old grant can move.
//  2. UPDATE every remaining old→new row that has no conflicting new row
//     (prefer keeping expired rows when UNIQUE allows).
//  3. Delete expired old rows that still cannot move (live new already owns
//     the pair) so Support reissue is not left pointing at the old blob.
//     Those grant IDs vanish (not returned as revoked).
//  4. Revoke only live old grants that still conflict with a live new grant.
//
// Returns moved (rows rewritten in step 2) and the live grants expired in
// step 4 so callers can publish download.grant.revoked after commit.
func (r *DownloadGrantRepo) RetargetAsset(ctx context.Context, oldAssetID, newAssetID string) (moved int, revoked []download.DownloadGrant, err error) {
	oldAssetID = strings.TrimSpace(oldAssetID)
	newAssetID = strings.TrimSpace(newAssetID)
	if oldAssetID == "" || newAssetID == "" {
		return 0, nil, apperror.Validation("old asset id and new asset id are required")
	}
	if oldAssetID == newAssetID {
		return 0, nil, nil
	}

	// Clear expired new-asset rows that would block the rewrite.
	const deleteExpiredNewQ = `
		DELETE FROM download_grants AS x
		WHERE x.asset_id = $2
		  AND x.expires_at <= NOW()
		  AND EXISTS (
			SELECT 1 FROM download_grants AS dg
			WHERE dg.order_item_id = x.order_item_id AND dg.asset_id = $1
		  )`
	if _, err := r.queryer().ExecContext(ctx, deleteExpiredNewQ, oldAssetID, newAssetID); err != nil {
		if mapped := mapDownloadGrantExecError(err); mapped != err {
			return 0, nil, mapped
		}
		return 0, nil, fmt.Errorf("download_grant_repo: delete expired new-asset conflict: %w", err)
	}

	// Move live and expired old→new when the UNIQUE pair is free.
	const retargetQ = `
		UPDATE download_grants AS dg
		SET asset_id = $2
		WHERE dg.asset_id = $1
		  AND NOT EXISTS (
			SELECT 1 FROM download_grants AS x
			WHERE x.order_item_id = dg.order_item_id AND x.asset_id = $2
		  )`
	res, err := r.queryer().ExecContext(ctx, retargetQ, oldAssetID, newAssetID)
	if err != nil {
		if mapped := mapDownloadGrantExecError(err); mapped != err {
			return 0, nil, mapped
		}
		return 0, nil, fmt.Errorf("download_grant_repo: retarget asset: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, nil, fmt.Errorf("download_grant_repo: retarget asset rows: %w", err)
	}
	moved = int(affected)

	// Drop expired old rows still blocked by a (live) new-asset grant.
	// Grant IDs deleted here vanish (not counted as moved or revoked).
	const deleteBlockedExpiredOldQ = `
		DELETE FROM download_grants AS dg
		WHERE dg.asset_id = $1
		  AND dg.expires_at <= NOW()
		  AND EXISTS (
			SELECT 1 FROM download_grants AS x
			WHERE x.order_item_id = dg.order_item_id AND x.asset_id = $2
		  )`
	if _, err := r.queryer().ExecContext(ctx, deleteBlockedExpiredOldQ, oldAssetID, newAssetID); err != nil {
		if mapped := mapDownloadGrantExecError(err); mapped != err {
			return 0, nil, mapped
		}
		return 0, nil, fmt.Errorf("download_grant_repo: delete blocked expired old: %w", err)
	}

	// Revoke live old only when a live new-asset grant already exists.
	revokeQ := `
		UPDATE download_grants AS dg
		SET expires_at = NOW()
		WHERE dg.asset_id = $1
		  AND dg.expires_at > NOW()
		  AND EXISTS (
			SELECT 1 FROM download_grants AS x
			WHERE x.order_item_id = dg.order_item_id
			  AND x.asset_id = $2
			  AND x.expires_at > NOW()
		  )
		RETURNING ` + downloadGrantSelectColumns
	rows, err := r.queryer().QueryContext(ctx, revokeQ, oldAssetID, newAssetID)
	if err != nil {
		if mapped := mapDownloadGrantExecError(err); mapped != err {
			return 0, nil, mapped
		}
		return 0, nil, fmt.Errorf("download_grant_repo: revoke live conflict: %w", err)
	}
	defer rows.Close()
	revoked = []download.DownloadGrant{}
	for rows.Next() {
		g, scanErr := scanDownloadGrant(rows)
		if scanErr != nil {
			return 0, nil, fmt.Errorf("download_grant_repo: revoke conflict scan: %w", scanErr)
		}
		revoked = append(revoked, *g)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("download_grant_repo: revoke conflict rows: %w", err)
	}
	return moved, revoked, nil
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
// ON DELETE RESTRICT still applies to live grants — call
// DeleteExpiredOrRevokedByAssetID before media.Delete.
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

// DeleteExpiredOrRevokedByAssetID removes non-live grant rows for assetID.
func (r *DownloadGrantRepo) DeleteExpiredOrRevokedByAssetID(ctx context.Context, assetID string) (int, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return 0, apperror.Validation("asset id must not be empty")
	}
	const q = `DELETE FROM download_grants WHERE asset_id = $1 AND expires_at <= NOW()`
	res, err := r.queryer().ExecContext(ctx, q, assetID)
	if err != nil {
		return 0, fmt.Errorf("download_grant_repo: delete expired/revoked: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("download_grant_repo: delete expired/revoked rows: %w", err)
	}
	return int(n), nil
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
