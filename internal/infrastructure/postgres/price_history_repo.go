package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// Compile-time check that PriceHistoryRepo implements pricing.PriceHistoryRepository.
var _ pricing.PriceHistoryRepository = (*PriceHistoryRepo)(nil)

// PriceHistoryRepo implements pricing.PriceHistoryRepository using PostgreSQL.
type PriceHistoryRepo struct {
	db *sql.DB
}

// NewPriceHistoryRepo returns a new PriceHistoryRepo backed by db.
func NewPriceHistoryRepo(db *sql.DB) (*PriceHistoryRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewPriceHistoryRepo: nil *sql.DB")
	}
	return &PriceHistoryRepo{db: db}, nil
}

// Record inserts a new price snapshot.
func (r *PriceHistoryRepo) Record(ctx context.Context, s *pricing.PriceSnapshot) error {
	if s == nil {
		return fmt.Errorf("price_history_repo: record: snapshot must not be nil")
	}
	const q = `INSERT INTO price_history (id, variant_id, store_id, currency, amount, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := r.db.ExecContext(ctx, q,
		s.ID, s.VariantID, s.StoreID, s.Amount.Currency(), s.Amount.Amount(), s.RecordedAt,
	)
	if err != nil {
		return fmt.Errorf("price_history_repo: record: %w", err)
	}
	return nil
}

// LowestSince returns the snapshot with the lowest amount for the given
// variant, currency, and store recorded on or after since.
// Returns (nil, nil) when no snapshots exist in the window.
func (r *PriceHistoryRepo) LowestSince(ctx context.Context, variantID, currency, storeID string, since time.Time) (*pricing.PriceSnapshot, error) {
	const q = `SELECT id, variant_id, store_id, currency, amount, recorded_at
		FROM price_history
		WHERE variant_id = $1 AND currency = $2 AND store_id = $3 AND recorded_at >= $4
		ORDER BY amount ASC, recorded_at ASC
		LIMIT 1`

	var s pricing.PriceSnapshot
	var amount int64
	var cur string
	err := r.db.QueryRowContext(ctx, q, variantID, currency, storeID, since).Scan(
		&s.ID, &s.VariantID, &s.StoreID, &cur, &amount, &s.RecordedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("price_history_repo: lowest since: %w", err)
	}
	m, err := shared.NewMoney(amount, cur)
	if err != nil {
		return nil, fmt.Errorf("price_history_repo: reconstruct money: %w", err)
	}
	s.Amount = m
	return &s, nil
}
