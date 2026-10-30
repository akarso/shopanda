package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ download.SettingsRepository = (*DownloadableGrantSettingsRepo)(nil)

// DownloadableGrantSettingsRepo persists optional per-product grant overrides.
type DownloadableGrantSettingsRepo struct {
	db *sql.DB
	tx *sql.Tx
}

type downloadableGrantSettingsQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewDownloadableGrantSettingsRepo returns a settings repo backed by db.
func NewDownloadableGrantSettingsRepo(db *sql.DB) (*DownloadableGrantSettingsRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewDownloadableGrantSettingsRepo: nil *sql.DB")
	}
	return &DownloadableGrantSettingsRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *DownloadableGrantSettingsRepo) WithTx(tx *sql.Tx) download.SettingsRepository {
	return &DownloadableGrantSettingsRepo{db: r.db, tx: tx}
}

func (r *DownloadableGrantSettingsRepo) queryer() downloadableGrantSettingsQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func mapGrantSettingsExecError(err error) error {
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
		return apperror.Validation("downloadable grant settings constraint violation")
	case "22P02":
		return apperror.Validation("invalid product id")
	default:
		return err
	}
}

// FindByProductID returns settings or (nil, nil).
func (r *DownloadableGrantSettingsRepo) FindByProductID(ctx context.Context, productID string) (*download.GrantSettings, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return nil, nil
	}
	const q = `SELECT product_id, expiry_days, download_limit
		FROM downloadable_grant_settings WHERE product_id = $1`
	var (
		s          download.GrantSettings
		expiryDays sql.NullInt64
		limit      sql.NullInt64
	)
	err := r.queryer().QueryRowContext(ctx, q, productID).Scan(&s.ProductID, &expiryDays, &limit)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		if mapped := mapGrantSettingsExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("downloadable_grant_settings_repo: find: %w", err)
	}
	if expiryDays.Valid {
		v := int(expiryDays.Int64)
		s.ExpiryDays = &v
	}
	if limit.Valid {
		v := int(limit.Int64)
		s.DownloadLimit = &v
	}
	return &s, nil
}

// Upsert inserts or replaces the override row.
func (r *DownloadableGrantSettingsRepo) Upsert(ctx context.Context, s *download.GrantSettings) error {
	if s == nil {
		return apperror.Validation("download grant settings must not be nil")
	}
	if err := s.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO downloadable_grant_settings (product_id, expiry_days, download_limit)
		VALUES ($1, $2, $3)
		ON CONFLICT (product_id) DO UPDATE
		SET expiry_days = EXCLUDED.expiry_days, download_limit = EXCLUDED.download_limit`
	_, err := r.queryer().ExecContext(ctx, q, s.ProductID, nullIntPtr(s.ExpiryDays), nullIntPtr(s.DownloadLimit))
	if err != nil {
		if mapped := mapGrantSettingsExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("downloadable_grant_settings_repo: upsert: %w", err)
	}
	return nil
}

// Remove deletes the override row.
func (r *DownloadableGrantSettingsRepo) Remove(ctx context.Context, productID string) error {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return fmt.Errorf("downloadable_grant_settings_repo: remove: empty product id")
	}
	const q = `DELETE FROM downloadable_grant_settings WHERE product_id = $1`
	res, err := r.queryer().ExecContext(ctx, q, productID)
	if err != nil {
		if mapped := mapGrantSettingsExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("downloadable_grant_settings_repo: remove: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("downloadable_grant_settings_repo: remove rows: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("downloadable grant settings not found")
	}
	return nil
}

func nullIntPtr(v *int) interface{} {
	if v == nil {
		return nil
	}
	return *v
}
