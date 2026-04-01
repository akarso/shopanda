package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// Compile-time check that PriceRepo implements pricing.PriceRepository.
var _ pricing.PriceRepository = (*PriceRepo)(nil)

// PriceRepo implements pricing.PriceRepository using PostgreSQL.
type PriceRepo struct {
	db *sql.DB
}

// NewPriceRepo returns a new PriceRepo backed by db.
func NewPriceRepo(db *sql.DB) *PriceRepo {
	return &PriceRepo{db: db}
}

// FindByVariantAndCurrency returns the price for a variant in the given currency.
// Returns (nil, nil) when no price exists.
func (r *PriceRepo) FindByVariantAndCurrency(ctx context.Context, variantID, currency string) (*pricing.Price, error) {
	const q = `SELECT id, variant_id, currency, amount, created_at
		FROM prices WHERE variant_id = $1 AND currency = $2`

	var p pricing.Price
	var amount int64
	var cur string
	err := r.db.QueryRowContext(ctx, q, variantID, currency).Scan(
		&p.ID, &p.VariantID, &cur, &amount, &p.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("price_repo: find by variant and currency: %w", err)
	}
	m, err := shared.NewMoney(amount, cur)
	if err != nil {
		return nil, fmt.Errorf("price_repo: reconstruct money: %w", err)
	}
	p.Amount = m
	return &p, nil
}

// ListByVariantID returns all prices for a variant.
func (r *PriceRepo) ListByVariantID(ctx context.Context, variantID string) ([]pricing.Price, error) {
	const q = `SELECT id, variant_id, currency, amount, created_at
		FROM prices WHERE variant_id = $1 ORDER BY currency`

	rows, err := r.db.QueryContext(ctx, q, variantID)
	if err != nil {
		return nil, fmt.Errorf("price_repo: list by variant: %w", err)
	}
	defer rows.Close()

	var prices []pricing.Price
	for rows.Next() {
		var p pricing.Price
		var amount int64
		var cur string
		if err := rows.Scan(&p.ID, &p.VariantID, &cur, &amount, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("price_repo: list scan: %w", err)
		}
		m, err := shared.NewMoney(amount, cur)
		if err != nil {
			return nil, fmt.Errorf("price_repo: reconstruct money: %w", err)
		}
		p.Amount = m
		prices = append(prices, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("price_repo: list rows: %w", err)
	}
	return prices, nil
}

// Upsert creates or updates a price for a variant+currency pair.
func (r *PriceRepo) Upsert(ctx context.Context, p *pricing.Price) error {
	if p == nil {
		return fmt.Errorf("price_repo: upsert: price must not be nil")
	}
	const q = `INSERT INTO prices (id, variant_id, currency, amount, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (variant_id, currency) DO UPDATE
		SET amount = EXCLUDED.amount`

	_, err := r.db.ExecContext(ctx, q,
		p.ID, p.VariantID, p.Amount.Currency(), p.Amount.Amount(), p.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("price_repo: upsert: %w", err)
	}
	return nil
}
