package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/voucher"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ voucher.RedemptionRepository = (*VoucherRedemptionRepo)(nil)

// VoucherRedemptionRepo persists redeem/unredeem audit rows.
type VoucherRedemptionRepo struct {
	db *sql.DB
	tx *sql.Tx
}

type voucherRedemptionQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewVoucherRedemptionRepo returns a redemption repo backed by db.
func NewVoucherRedemptionRepo(db *sql.DB) (*VoucherRedemptionRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewVoucherRedemptionRepo: nil *sql.DB")
	}
	return &VoucherRedemptionRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *VoucherRedemptionRepo) WithTx(tx *sql.Tx) voucher.RedemptionRepository {
	return &VoucherRedemptionRepo{db: r.db, tx: tx}
}

func (r *VoucherRedemptionRepo) queryer() voucherRedemptionQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func mapVoucherRedemptionExecError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23503":
		return apperror.NotFound("voucher not found")
	case "23514":
		return apperror.Validation("voucher redemption constraint violation")
	case "23505":
		return apperror.Conflict("voucher redemption already exists")
	case "22P02":
		return apperror.Validation("invalid voucher redemption id")
	default:
		return err
	}
}

// Insert writes a redemption audit row.
func (r *VoucherRedemptionRepo) Insert(ctx context.Context, row *voucher.Redemption) error {
	if row == nil {
		return apperror.Validation("voucher redemption must not be nil")
	}
	if err := row.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO voucher_redemptions (id, voucher_id, action, actor_id, note, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.queryer().ExecContext(ctx, q,
		row.ID, row.VoucherID, row.Action, row.ActorID, row.Note, row.CreatedAt,
	)
	if err != nil {
		if mapped := mapVoucherRedemptionExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("voucher_redemption_repo: insert: %w", err)
	}
	return nil
}

// ListByVoucherID returns redemptions newest first.
func (r *VoucherRedemptionRepo) ListByVoucherID(ctx context.Context, voucherID string) ([]voucher.Redemption, error) {
	voucherID = strings.TrimSpace(voucherID)
	if voucherID == "" {
		return []voucher.Redemption{}, nil
	}
	const q = `SELECT id, voucher_id, action, actor_id, note, created_at
		FROM voucher_redemptions WHERE voucher_id = $1
		ORDER BY created_at DESC, id DESC`
	rows, err := r.queryer().QueryContext(ctx, q, voucherID)
	if err != nil {
		return nil, fmt.Errorf("voucher_redemption_repo: list: %w", err)
	}
	defer rows.Close()
	out := make([]voucher.Redemption, 0)
	for rows.Next() {
		var row voucher.Redemption
		if err := rows.Scan(&row.ID, &row.VoucherID, &row.Action, &row.ActorID, &row.Note, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("voucher_redemption_repo: scan: %w", err)
		}
		row.CreatedAt = row.CreatedAt.UTC()
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("voucher_redemption_repo: rows: %w", err)
	}
	return out, nil
}
