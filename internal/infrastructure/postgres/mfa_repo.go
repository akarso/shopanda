package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	domainMFA "github.com/akarso/shopanda/internal/domain/mfa"
	"github.com/akarso/shopanda/internal/platform/id"
)

var _ domainMFA.Repository = (*MFARepo)(nil)

// MFARepo implements mfa.Repository using PostgreSQL.
type MFARepo struct {
	db *sql.DB
}

// NewMFARepo returns an MFARepo backed by db.
func NewMFARepo(db *sql.DB) (*MFARepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewMFARepo: nil *sql.DB")
	}
	return &MFARepo{db: db}, nil
}

// GetState returns MFA enrollment state for a customer.
func (r *MFARepo) GetState(ctx context.Context, customerID string) (domainMFA.State, error) {
	const q = `SELECT totp_secret_enc, totp_confirmed_at FROM customers WHERE id = $1`
	var secretEnc sql.NullString
	var confirmedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, q, customerID).Scan(&secretEnc, &confirmedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domainMFA.State{}, nil
	}
	if err != nil {
		return domainMFA.State{}, fmt.Errorf("mfa_repo: get state: %w", err)
	}
	state := domainMFA.State{}
	if secretEnc.Valid {
		state.SecretEnc = secretEnc.String
	}
	if confirmedAt.Valid {
		t := confirmedAt.Time.UTC()
		state.ConfirmedAt = &t
	}
	return state, nil
}

// SavePendingSecret stores an unconfirmed encrypted TOTP secret.
func (r *MFARepo) SavePendingSecret(ctx context.Context, customerID, secretEnc string) error {
	const q = `UPDATE customers
		SET totp_secret_enc = $1, totp_confirmed_at = NULL, updated_at = $2
		WHERE id = $3`
	res, err := r.db.ExecContext(ctx, q, secretEnc, time.Now().UTC(), customerID)
	if err != nil {
		return fmt.Errorf("mfa_repo: save pending secret: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mfa_repo: save pending secret rows: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("mfa_repo: customer not found")
	}
	return nil
}

// ConfirmEnrollment marks TOTP as active.
func (r *MFARepo) ConfirmEnrollment(ctx context.Context, customerID string, confirmedAt time.Time) error {
	const q = `UPDATE customers
		SET totp_confirmed_at = $1, updated_at = $2
		WHERE id = $3 AND totp_secret_enc IS NOT NULL`
	res, err := r.db.ExecContext(ctx, q, confirmedAt.UTC(), time.Now().UTC(), customerID)
	if err != nil {
		return fmt.Errorf("mfa_repo: confirm enrollment: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mfa_repo: confirm enrollment rows: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("mfa_repo: enrollment not pending")
	}
	return nil
}

// ClearEnrollment removes MFA state for a customer.
func (r *MFARepo) ClearEnrollment(ctx context.Context, customerID string) error {
	const q = `UPDATE customers
		SET totp_secret_enc = NULL, totp_confirmed_at = NULL, updated_at = $1
		WHERE id = $2`
	if _, err := r.db.ExecContext(ctx, q, time.Now().UTC(), customerID); err != nil {
		return fmt.Errorf("mfa_repo: clear enrollment: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM admin_mfa_recovery_codes WHERE customer_id = $1`, customerID); err != nil {
		return fmt.Errorf("mfa_repo: clear recovery codes: %w", err)
	}
	return nil
}

// ReplaceRecoveryCodes replaces all recovery codes for a customer.
func (r *MFARepo) ReplaceRecoveryCodes(ctx context.Context, customerID string, codeHashes []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mfa_repo: replace recovery codes begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_mfa_recovery_codes WHERE customer_id = $1`, customerID); err != nil {
		return fmt.Errorf("mfa_repo: delete recovery codes: %w", err)
	}
	const insertQ = `INSERT INTO admin_mfa_recovery_codes (id, customer_id, code_hash, created_at)
		VALUES ($1, $2, $3, $4)`
	now := time.Now().UTC()
	for _, hash := range codeHashes {
		if _, err := tx.ExecContext(ctx, insertQ, id.New(), customerID, hash, now); err != nil {
			return fmt.Errorf("mfa_repo: insert recovery code: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mfa_repo: replace recovery codes commit: %w", err)
	}
	return nil
}

// ConsumeRecoveryCode marks a matching unused recovery code as used.
func (r *MFARepo) ConsumeRecoveryCode(ctx context.Context, customerID, codeHash string) (bool, error) {
	const q = `UPDATE admin_mfa_recovery_codes
		SET used_at = $1
		WHERE customer_id = $2 AND code_hash = $3 AND used_at IS NULL`
	res, err := r.db.ExecContext(ctx, q, time.Now().UTC(), customerID, codeHash)
	if err != nil {
		return false, fmt.Errorf("mfa_repo: consume recovery code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mfa_repo: consume recovery code rows: %w", err)
	}
	return n > 0, nil
}
