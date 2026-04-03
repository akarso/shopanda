package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
)

// Compile-time check.
var _ customer.PasswordResetRepository = (*ResetTokenRepo)(nil)

// ResetTokenRepo implements customer.PasswordResetRepository using PostgreSQL.
type ResetTokenRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// NewResetTokenRepo returns a new ResetTokenRepo backed by db.
func NewResetTokenRepo(db *sql.DB) *ResetTokenRepo {
	return &ResetTokenRepo{db: db}
}

// Create persists a new password reset token.
func (r *ResetTokenRepo) Create(ctx context.Context, t *customer.PasswordResetToken) error {
	const q = `INSERT INTO password_reset_tokens (id, customer_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`

	_, err := r.exec(ctx, q, t.ID, t.CustomerID, t.TokenHash, t.ExpiresAt, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("reset_token_repo: create: %w", err)
	}
	return nil
}

// FindByTokenHash returns a reset token by its hash.
func (r *ResetTokenRepo) FindByTokenHash(ctx context.Context, hash string) (*customer.PasswordResetToken, error) {
	const q = `SELECT id, customer_id, token_hash, expires_at, used_at, created_at
		FROM password_reset_tokens WHERE token_hash = $1`

	row := r.queryRow(ctx, q, hash)
	var t customer.PasswordResetToken
	var usedAt sql.NullTime

	err := row.Scan(&t.ID, &t.CustomerID, &t.TokenHash, &t.ExpiresAt, &usedAt, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reset_token_repo: find by hash: %w", err)
	}
	if usedAt.Valid {
		t.UsedAt = &usedAt.Time
	}
	return &t, nil
}

// MarkUsed sets the used_at timestamp on a reset token.
func (r *ResetTokenRepo) MarkUsed(ctx context.Context, id string) error {
	const q = `UPDATE password_reset_tokens SET used_at = $1 WHERE id = $2`
	_, err := r.exec(ctx, q, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("reset_token_repo: mark used: %w", err)
	}
	return nil
}

func (r *ResetTokenRepo) queryRow(ctx context.Context, q string, args ...interface{}) *sql.Row {
	if r.tx != nil {
		return r.tx.QueryRowContext(ctx, q, args...)
	}
	return r.db.QueryRowContext(ctx, q, args...)
}

func (r *ResetTokenRepo) exec(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	if r.tx != nil {
		return r.tx.ExecContext(ctx, q, args...)
	}
	return r.db.ExecContext(ctx, q, args...)
}
