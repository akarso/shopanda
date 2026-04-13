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
	tx *sql.Tx
}

// NewPriceRepo returns a new PriceRepo backed by db.
func NewPriceRepo(db *sql.DB) (*PriceRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewPriceRepo: nil *sql.DB")
	}
	return &PriceRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *PriceRepo) WithTx(tx *sql.Tx) pricing.PriceRepository {
	return &PriceRepo{db: r.db, tx: tx}
}

// FindByVariantCurrencyAndStore returns the price for a variant in the given
// currency and store. An empty storeID means the global/default price.
// Returns (nil, nil) when no price exists.
func (r *PriceRepo) FindByVariantCurrencyAndStore(ctx context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	const q = `SELECT id, variant_id, store_id, currency, amount, created_at
		FROM prices WHERE variant_id = $1 AND currency = $2 AND store_id = $3`

	var p pricing.Price
	var amount int64
	var cur string
	err := r.queryRow(ctx, q, variantID, currency, storeID).Scan(
		&p.ID, &p.VariantID, &p.StoreID, &cur, &amount, &p.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("price_repo: find by variant currency and store: %w", err)
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
	const q = `SELECT id, variant_id, store_id, currency, amount, created_at
		FROM prices WHERE variant_id = $1 ORDER BY currency, store_id`

	rows, err := r.query(ctx, q, variantID)
	if err != nil {
		return nil, fmt.Errorf("price_repo: list by variant: %w", err)
	}
	defer rows.Close()

	var prices []pricing.Price
	for rows.Next() {
		var p pricing.Price
		var amount int64
		var cur string
		if err := rows.Scan(&p.ID, &p.VariantID, &p.StoreID, &cur, &amount, &p.CreatedAt); err != nil {
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

// Upsert creates or updates a price for a variant+currency+store tuple.
func (r *PriceRepo) Upsert(ctx context.Context, p *pricing.Price) error {
	if p == nil {
		return fmt.Errorf("price_repo: upsert: price must not be nil")
	}
	const q = `INSERT INTO prices (id, variant_id, store_id, currency, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (variant_id, currency, store_id) DO UPDATE
		SET amount = EXCLUDED.amount,
		    id = EXCLUDED.id,
		    created_at = EXCLUDED.created_at`

	_, err := r.exec(ctx, q,
		p.ID, p.VariantID, p.StoreID, p.Amount.Currency(), p.Amount.Amount(), p.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("price_repo: upsert: %w", err)
	}
	return nil
}

// List returns a page of prices ordered by variant_id then currency.
func (r *PriceRepo) List(ctx context.Context, offset, limit int) ([]pricing.Price, error) {
	if offset < 0 {
		return nil, fmt.Errorf("price_repo: list: negative offset")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("price_repo: list: non-positive limit")
	}
	if limit > 100 {
		limit = 100
	}

	const q = `SELECT id, variant_id, store_id, currency, amount, created_at
		FROM prices ORDER BY variant_id, currency, store_id
		LIMIT $1 OFFSET $2`

	rows, err := r.query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("price_repo: list: %w", err)
	}
	defer rows.Close()

	var prices []pricing.Price
	for rows.Next() {
		var p pricing.Price
		var amount int64
		var cur string
		if err := rows.Scan(&p.ID, &p.VariantID, &p.StoreID, &cur, &amount, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("price_repo: list scan: %w", err)
		}
		m, err := shared.NewMoney(amount, cur)
		if err != nil {
			return nil, fmt.Errorf("price_repo: list reconstruct money: %w", err)
		}
		p.Amount = m
		prices = append(prices, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("price_repo: list rows: %w", err)
	}
	return prices, nil
}

// queryRow delegates to tx or db.
func (r *PriceRepo) queryRow(ctx context.Context, q string, args ...interface{}) *sql.Row {
	if r.tx != nil {
		return r.tx.QueryRowContext(ctx, q, args...)
	}
	return r.db.QueryRowContext(ctx, q, args...)
}

// query delegates to tx or db.
func (r *PriceRepo) query(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	if r.tx != nil {
		return r.tx.QueryContext(ctx, q, args...)
	}
	return r.db.QueryContext(ctx, q, args...)
}

// exec delegates to tx or db.
func (r *PriceRepo) exec(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	if r.tx != nil {
		return r.tx.ExecContext(ctx, q, args...)
	}
	return r.db.ExecContext(ctx, q, args...)
}
