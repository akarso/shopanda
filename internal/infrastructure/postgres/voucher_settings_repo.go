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

var _ voucher.SettingsRepository = (*VoucherSettingsRepo)(nil)

// VoucherSettingsRepo persists per-product voucher flags/config.
type VoucherSettingsRepo struct {
	db *sql.DB
	tx *sql.Tx
}

type voucherSettingsQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewVoucherSettingsRepo returns a settings repo backed by db.
func NewVoucherSettingsRepo(db *sql.DB) (*VoucherSettingsRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewVoucherSettingsRepo: nil *sql.DB")
	}
	return &VoucherSettingsRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *VoucherSettingsRepo) WithTx(tx *sql.Tx) voucher.SettingsRepository {
	return &VoucherSettingsRepo{db: r.db, tx: tx}
}

func (r *VoucherSettingsRepo) queryer() voucherSettingsQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func mapVoucherSettingsExecError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23503":
		return apperror.NotFound("product not found")
	case "23514":
		return apperror.Validation("voucher settings constraint violation")
	case "22P02":
		return apperror.Validation("invalid product id")
	default:
		return err
	}
}

// FindByProductID returns settings or (nil, nil).
func (r *VoucherSettingsRepo) FindByProductID(ctx context.Context, productID string) (*voucher.Settings, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return nil, nil
	}
	const q = `SELECT product_id, enabled, codes_per_unit, max_redemptions
		FROM voucher_settings WHERE product_id = $1`
	var s voucher.Settings
	err := r.queryer().QueryRowContext(ctx, q, productID).Scan(
		&s.ProductID, &s.Enabled, &s.CodesPerUnit, &s.MaxRedemptions,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		if mapped := mapVoucherSettingsExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("voucher_settings_repo: find: %w", err)
	}
	return &s, nil
}

// Upsert inserts or replaces the settings row.
func (r *VoucherSettingsRepo) Upsert(ctx context.Context, s *voucher.Settings) error {
	if s == nil {
		return apperror.Validation("voucher settings must not be nil")
	}
	if err := s.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO voucher_settings (product_id, enabled, codes_per_unit, max_redemptions)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (product_id) DO UPDATE
		SET enabled = EXCLUDED.enabled,
		    codes_per_unit = EXCLUDED.codes_per_unit,
		    max_redemptions = EXCLUDED.max_redemptions`
	_, err := r.queryer().ExecContext(ctx, q, s.ProductID, s.Enabled, s.CodesPerUnit, s.MaxRedemptions)
	if err != nil {
		if mapped := mapVoucherSettingsExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("voucher_settings_repo: upsert: %w", err)
	}
	return nil
}

// Remove deletes the settings row.
func (r *VoucherSettingsRepo) Remove(ctx context.Context, productID string) error {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return fmt.Errorf("voucher_settings_repo: remove: empty product id")
	}
	const q = `DELETE FROM voucher_settings WHERE product_id = $1`
	res, err := r.queryer().ExecContext(ctx, q, productID)
	if err != nil {
		if mapped := mapVoucherSettingsExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("voucher_settings_repo: remove: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("voucher_settings_repo: remove rows: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("voucher settings not found")
	}
	return nil
}
