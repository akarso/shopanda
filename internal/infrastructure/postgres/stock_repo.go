package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
func NewStockRepo(db *sql.DB) (*StockRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewStockRepo: nil *sql.DB")
	}
	return &StockRepo{db: db}, nil
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

// ListStock returns a page of stock entries ordered by variant_id.
func (r *StockRepo) ListStock(ctx context.Context, offset, limit int) ([]inventory.StockEntry, error) {
	if offset < 0 {
		return nil, fmt.Errorf("stock_repo: list stock: negative offset")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("stock_repo: list stock: non-positive limit")
	}
	if limit > 100 {
		limit = 100
	}

	const q = `SELECT variant_id, quantity, updated_at FROM stock ORDER BY variant_id LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("stock_repo: list stock: %w", err)
	}
	defer rows.Close()

	var entries []inventory.StockEntry
	for rows.Next() {
		var s inventory.StockEntry
		if err := rows.Scan(&s.VariantID, &s.Quantity, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("stock_repo: list stock: scan: %w", err)
		}
		entries = append(entries, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stock_repo: list stock: rows: %w", err)
	}
	return entries, nil
}

// ListInventory returns a paginated admin inventory view for all variants.
func (r *StockRepo) ListInventory(ctx context.Context, offset, limit int, search string) ([]inventory.InventoryListItem, error) {
	if offset < 0 {
		return nil, fmt.Errorf("stock_repo: list inventory: negative offset")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("stock_repo: list inventory: non-positive limit")
	}
	if limit > 100 {
		limit = 100
	}

	search = strings.TrimSpace(search)
	pattern := ""
	if search != "" {
		pattern = "%" + search + "%"
	}

	const q = `
		SELECT
			v.id,
			v.product_id,
			v.sku,
			p.name,
			COALESCE(v.name, ''),
			COALESCE(s.quantity, 0),
			COALESCE(reserved.reserved_qty, 0),
			COALESCE(s.updated_at, to_timestamp(0))
		FROM variants v
		INNER JOIN products p ON p.id = v.product_id
		LEFT JOIN stock s ON s.variant_id = v.id
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(r.quantity), 0) AS reserved_qty
			FROM reservations r
			WHERE r.variant_id = v.id
			  AND r.status = 'active'
			  AND r.expires_at > now()
		) reserved ON TRUE
		WHERE ($3 = '' OR v.sku ILIKE $4 OR COALESCE(v.name, '') ILIKE $4 OR p.name ILIKE $4)
		ORDER BY v.sku
		LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, q, limit, offset, search, pattern)
	if err != nil {
		return nil, fmt.Errorf("stock_repo: list inventory: %w", err)
	}
	defer rows.Close()

	var items []inventory.InventoryListItem
	for rows.Next() {
		var item inventory.InventoryListItem
		if err := rows.Scan(
			&item.VariantID,
			&item.ProductID,
			&item.SKU,
			&item.ProductName,
			&item.VariantName,
			&item.Quantity,
			&item.Reserved,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("stock_repo: list inventory: scan: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stock_repo: list inventory: rows: %w", err)
	}
	return items, nil
}
