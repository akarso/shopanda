package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/legal"
)

// Compile-time check that ConsentRepo implements legal.ConsentRepository.
var _ legal.ConsentRepository = (*ConsentRepo)(nil)

// ConsentRepo implements legal.ConsentRepository using PostgreSQL.
type ConsentRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// NewConsentRepo returns a new ConsentRepo backed by db.
func NewConsentRepo(db *sql.DB) (*ConsentRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewConsentRepo: nil *sql.DB")
	}
	return &ConsentRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *ConsentRepo) WithTx(tx *sql.Tx) legal.ConsentRepository {
	return &ConsentRepo{db: r.db, tx: tx}
}

// FindByCustomerID returns the consent for a customer.
// Returns (nil, nil) when not found.
func (r *ConsentRepo) FindByCustomerID(ctx context.Context, customerID string) (*legal.Consent, error) {
	const q = `SELECT customer_id, necessary, analytics, marketing, updated_at
		FROM consents WHERE customer_id = $1`

	var c legal.Consent
	err := r.queryRow(ctx, q, customerID).Scan(
		&c.CustomerID, &c.Necessary, &c.Analytics, &c.Marketing, &c.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consent_repo: find by customer id: %w", err)
	}
	return &c, nil
}

// Upsert creates or updates a consent record.
func (r *ConsentRepo) Upsert(ctx context.Context, c *legal.Consent) error {
	if c == nil {
		return fmt.Errorf("consent_repo: upsert: consent must not be nil")
	}
	now := time.Now().UTC()
	const q = `INSERT INTO consents (customer_id, necessary, analytics, marketing, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (customer_id) DO UPDATE
		SET necessary = EXCLUDED.necessary,
			analytics = EXCLUDED.analytics,
			marketing = EXCLUDED.marketing,
			updated_at = EXCLUDED.updated_at`

	_, err := r.exec(ctx, q, c.CustomerID, c.Necessary, c.Analytics, c.Marketing, now)
	if err != nil {
		return fmt.Errorf("consent_repo: upsert: %w", err)
	}
	c.UpdatedAt = now
	return nil
}

// DeleteByCustomerID removes the consent record for a customer.
func (r *ConsentRepo) DeleteByCustomerID(ctx context.Context, customerID string) error {
	const q = `DELETE FROM consents WHERE customer_id = $1`
	_, err := r.exec(ctx, q, customerID)
	if err != nil {
		return fmt.Errorf("consent_repo: delete: %w", err)
	}
	return nil
}

// queryRow delegates to tx or db.
func (r *ConsentRepo) queryRow(ctx context.Context, q string, args ...interface{}) *sql.Row {
	if r.tx != nil {
		return r.tx.QueryRowContext(ctx, q, args...)
	}
	return r.db.QueryRowContext(ctx, q, args...)
}

// exec delegates to tx or db.
func (r *ConsentRepo) exec(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	if r.tx != nil {
		return r.tx.ExecContext(ctx, q, args...)
	}
	return r.db.ExecContext(ctx, q, args...)
}
