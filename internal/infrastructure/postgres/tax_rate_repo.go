package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/tax"
)

// Compile-time check that TaxRateRepo implements tax.RateRepository.
var _ tax.RateRepository = (*TaxRateRepo)(nil)

// TaxRateRepo implements tax.RateRepository using PostgreSQL.
type TaxRateRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// NewTaxRateRepo returns a new TaxRateRepo backed by db.
func NewTaxRateRepo(db *sql.DB) (*TaxRateRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewTaxRateRepo: nil *sql.DB")
	}
	return &TaxRateRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *TaxRateRepo) WithTx(tx *sql.Tx) *TaxRateRepo {
	return &TaxRateRepo{db: r.db, tx: tx}
}

func (r *TaxRateRepo) queryRow(ctx context.Context, q string, args ...interface{}) *sql.Row {
	if r.tx != nil {
		return r.tx.QueryRowContext(ctx, q, args...)
	}
	return r.db.QueryRowContext(ctx, q, args...)
}

func (r *TaxRateRepo) query(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	if r.tx != nil {
		return r.tx.QueryContext(ctx, q, args...)
	}
	return r.db.QueryContext(ctx, q, args...)
}

func (r *TaxRateRepo) exec(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	if r.tx != nil {
		return r.tx.ExecContext(ctx, q, args...)
	}
	return r.db.ExecContext(ctx, q, args...)
}

// FindByCountryClassAndStore returns the rate for a country+class+store tuple.
// An empty storeID means the global/default rate.
// Returns (nil, nil) when no rate exists.
func (r *TaxRateRepo) FindByCountryClassAndStore(ctx context.Context, country, class, storeID string) (*tax.TaxRate, error) {
	const q = `SELECT id, country, class, store_id, rate
		FROM tax_rates WHERE country = $1 AND class = $2 AND store_id = $3`

	var tr tax.TaxRate
	err := r.queryRow(ctx, q, country, class, storeID).Scan(
		&tr.ID, &tr.Country, &tr.Class, &tr.StoreID, &tr.Rate,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tax_rate_repo: find by country class and store: %w", err)
	}
	return &tr, nil
}

// ListByCountry returns all rates for a country, ordered by class then store_id.
func (r *TaxRateRepo) ListByCountry(ctx context.Context, country string) ([]tax.TaxRate, error) {
	const q = `SELECT id, country, class, store_id, rate
		FROM tax_rates WHERE country = $1 ORDER BY class, store_id`

	rows, err := r.query(ctx, q, country)
	if err != nil {
		return nil, fmt.Errorf("tax_rate_repo: list by country: %w", err)
	}
	defer rows.Close()

	var rates []tax.TaxRate
	for rows.Next() {
		var tr tax.TaxRate
		if err := rows.Scan(&tr.ID, &tr.Country, &tr.Class, &tr.StoreID, &tr.Rate); err != nil {
			return nil, fmt.Errorf("tax_rate_repo: list scan: %w", err)
		}
		rates = append(rates, tr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tax_rate_repo: list rows: %w", err)
	}
	return rates, nil
}

// Upsert creates or updates a rate for a country+class+store tuple.
func (r *TaxRateRepo) Upsert(ctx context.Context, tr *tax.TaxRate) error {
	if tr == nil {
		return fmt.Errorf("tax_rate_repo: upsert: rate must not be nil")
	}
	const q = `INSERT INTO tax_rates (id, country, class, store_id, rate)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (country, class, store_id) DO UPDATE
		SET rate = EXCLUDED.rate
		RETURNING id`

	err := r.queryRow(ctx, q, tr.ID, tr.Country, tr.Class, tr.StoreID, tr.Rate).Scan(&tr.ID)
	if err != nil {
		return fmt.Errorf("tax_rate_repo: upsert: %w", err)
	}
	return nil
}

// Delete removes a tax rate by ID.
func (r *TaxRateRepo) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM tax_rates WHERE id = $1`
	res, err := r.exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("tax_rate_repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("tax_rate_repo: delete rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("tax_rate_repo: delete: rate %s not found", id)
	}
	return nil
}
