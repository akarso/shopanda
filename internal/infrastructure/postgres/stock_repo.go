package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
)

// Compile-time check that StockRepo implements inventory.StockRepository.
var _ inventory.StockRepository = (*StockRepo)(nil)

// StockRepo implements inventory.StockRepository using PostgreSQL.
type StockRepo struct {
	db *sql.DB
}

// NewStockRepo returns a new StockRepo backed by db.
func NewStockRepo(db *sql.DB) *StockRepo {
	return &StockRepo{db: db}
}

// GetStock returns the stock entry for a variant.
// Returns a zero-quantity entry when no record exists.
func (r *StockRepo) GetStock(ctx context.Context, variantID string) (inventory.StockEntry, error) {
	if variantID == "" {
		return inventory.StockEntry{}, fmt.Errorf("stock_repo: get stock: empty variantID")
	}
	const q = `SELECT variant_id, quantity, updated_at FROM stock WHERE variant_id = $1`

	var s inventory.StockEntry
	err := r.db.QueryRowContext(ctx, q, variantID).Scan(
		&s.VariantID, &s.Quantity, &s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return inventory.StockEntry{
			VariantID: variantID,
			Quantity:  0,
			UpdatedAt: time.Time{},
		}, nil
	}
	if err != nil {
		return inventory.StockEntry{}, fmt.Errorf("stock_repo: get stock: %w", err)
	}
	return s, nil
}

// SetStock upserts the stock quantity for a variant.
func (r *StockRepo) SetStock(ctx context.Context, entry *inventory.StockEntry) error {
	if entry == nil {
		return fmt.Errorf("stock_repo: set stock: entry must not be nil")
	}
	const q = `INSERT INTO stock (variant_id, quantity, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (variant_id) DO UPDATE
		SET quantity = EXCLUDED.quantity,
		    updated_at = EXCLUDED.updated_at`

	_, err := r.db.ExecContext(ctx, q,
		entry.VariantID, entry.Quantity, entry.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("stock_repo: set stock: %w", err)
	}
	return nil
}
