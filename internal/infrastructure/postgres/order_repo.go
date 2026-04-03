package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/lib/pq"
)

// Compile-time check that OrderRepo implements order.OrderRepository.
var _ order.OrderRepository = (*OrderRepo)(nil)

// OrderRepo implements order.OrderRepository using PostgreSQL.
type OrderRepo struct {
	db *sql.DB
}

// NewOrderRepo returns a new OrderRepo backed by db.
func NewOrderRepo(db *sql.DB) *OrderRepo {
	return &OrderRepo{db: db}
}

// orderScanner abstracts *sql.Row and *sql.Rows for shared hydration logic.
type orderScanner interface {
	Scan(dest ...interface{}) error
}

// hydrateOrder reads an order header from any scanner.
func (r *OrderRepo) hydrateOrder(s orderScanner) (*order.Order, error) {
	var o order.Order
	var status string
	var totalAmount int64
	var totalCurrency string
	err := s.Scan(&o.ID, &o.CustomerID, &status, &o.Currency,
		&totalAmount, &totalCurrency, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := o.SetStatusFromDB(status); err != nil {
		return nil, err
	}
	total, err := shared.NewMoney(totalAmount, totalCurrency)
	if err != nil {
		return nil, fmt.Errorf("order_repo: total money: %w", err)
	}
	o.TotalAmount = total
	return &o, nil
}

// FindByID returns an order with its items by ID.
// Returns (nil, nil) when not found.
func (r *OrderRepo) FindByID(ctx context.Context, id string) (*order.Order, error) {
	if id == "" {
		return nil, fmt.Errorf("order_repo: find: empty id")
	}
	const q = `SELECT id, customer_id, status, currency, total_amount, total_currency, created_at, updated_at
		FROM orders WHERE id = $1`
	o, err := r.hydrateOrder(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("order_repo: find by id: %w", err)
	}
	items, err := r.loadItems(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	if err := o.SetItemsFromDB(items); err != nil {
		return nil, fmt.Errorf("order_repo: set items: %w", err)
	}
	return o, nil
}

// FindByCustomerID returns all orders for a customer, newest first.
func (r *OrderRepo) FindByCustomerID(ctx context.Context, customerID string) ([]order.Order, error) {
	if customerID == "" {
		return nil, fmt.Errorf("order_repo: find by customer: empty customer id")
	}
	const q = `SELECT id, customer_id, status, currency, total_amount, total_currency, created_at, updated_at
		FROM orders WHERE customer_id = $1
		ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("order_repo: find by customer: %w", err)
	}
	defer rows.Close()

	var orders []order.Order
	var ids []string
	for rows.Next() {
		o, err := r.hydrateOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("order_repo: scan order: %w", err)
		}
		orders = append(orders, *o)
		ids = append(ids, o.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("order_repo: rows: %w", err)
	}
	if len(orders) == 0 {
		return orders, nil
	}

	itemMap, err := r.loadItemsBatch(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range orders {
		if err := orders[i].SetItemsFromDB(itemMap[orders[i].ID]); err != nil {
			return nil, fmt.Errorf("order_repo: set items: %w", err)
		}
	}
	return orders, nil
}

// Save persists an order and its items (insert-only).
func (r *OrderRepo) Save(ctx context.Context, o *order.Order) error {
	if o == nil {
		return fmt.Errorf("order_repo: save: order must not be nil")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("order_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	const insertOrder = `INSERT INTO orders (id, customer_id, status, currency, total_amount, total_currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err = tx.ExecContext(ctx, insertOrder,
		o.ID, o.CustomerID, string(o.Status()), o.Currency,
		o.TotalAmount.Amount(), o.TotalAmount.Currency(),
		o.CreatedAt, o.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("order_repo: insert order: %w", err)
	}

	items := o.Items()
	if len(items) > 0 {
		const insertItem = `INSERT INTO order_items (order_id, variant_id, sku, name, quantity, unit_price, currency, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
		stmt, err := tx.PrepareContext(ctx, insertItem)
		if err != nil {
			return fmt.Errorf("order_repo: prepare item insert: %w", err)
		}
		defer stmt.Close()
		for i := range items {
			item := &items[i]
			_, err = stmt.ExecContext(ctx,
				o.ID, item.VariantID, item.SKU, item.Name, item.Quantity,
				item.UnitPrice.Amount(), item.UnitPrice.Currency(),
				item.CreatedAt,
			)
			if err != nil {
				return fmt.Errorf("order_repo: insert item %q: %w", item.VariantID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("order_repo: commit: %w", err)
	}
	return nil
}

// UpdateStatus updates only the status and updated_at of an existing order.
func (r *OrderRepo) UpdateStatus(ctx context.Context, o *order.Order) error {
	if o == nil {
		return fmt.Errorf("order_repo: update status: order must not be nil")
	}
	const q = `UPDATE orders SET status = $1, updated_at = $2 WHERE id = $3`
	res, err := r.db.ExecContext(ctx, q, string(o.Status()), o.UpdatedAt, o.ID)
	if err != nil {
		return fmt.Errorf("order_repo: update status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("order_repo: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("order_repo: update status: order not found")
	}
	return nil
}

// loadItems fetches all items for a single order, ordered by created_at.
func (r *OrderRepo) loadItems(ctx context.Context, orderID string) ([]order.Item, error) {
	const q = `SELECT variant_id, sku, name, quantity, unit_price, currency, created_at
		FROM order_items WHERE order_id = $1
		ORDER BY created_at`
	rows, err := r.db.QueryContext(ctx, q, orderID)
	if err != nil {
		return nil, fmt.Errorf("order_repo: load items: %w", err)
	}
	defer rows.Close()
	return r.scanItems(rows)
}

// loadItemsBatch fetches items for multiple orders in a single query.
func (r *OrderRepo) loadItemsBatch(ctx context.Context, orderIDs []string) (map[string][]order.Item, error) {
	const q = `SELECT order_id, variant_id, sku, name, quantity, unit_price, currency, created_at
		FROM order_items WHERE order_id = ANY($1)
		ORDER BY order_id, created_at`
	rows, err := r.db.QueryContext(ctx, q, pq.Array(orderIDs))
	if err != nil {
		return nil, fmt.Errorf("order_repo: load items batch: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]order.Item)
	for rows.Next() {
		var orderID, variantID, sku, name string
		var quantity int
		var priceAmount int64
		var priceCurrency string
		var createdAt time.Time
		if err := rows.Scan(&orderID, &variantID, &sku, &name, &quantity, &priceAmount, &priceCurrency, &createdAt); err != nil {
			return nil, fmt.Errorf("order_repo: scan batch item: %w", err)
		}
		unitPrice, err := shared.NewMoney(priceAmount, priceCurrency)
		if err != nil {
			return nil, fmt.Errorf("order_repo: batch item money: %w", err)
		}
		// Construct Item directly (bypassing NewItem) because DB data was
		// validated on write and we must preserve the stored CreatedAt.
		// SetItemsFromDB validates the total on the caller side.
		result[orderID] = append(result[orderID], order.Item{
			VariantID: variantID,
			SKU:       sku,
			Name:      name,
			Quantity:  quantity,
			UnitPrice: unitPrice,
			CreatedAt: createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("order_repo: batch items rows: %w", err)
	}
	return result, nil
}

// scanItems reads item rows into a slice.
func (r *OrderRepo) scanItems(rows *sql.Rows) ([]order.Item, error) {
	var items []order.Item
	for rows.Next() {
		var variantID, sku, name string
		var quantity int
		var priceAmount int64
		var priceCurrency string
		var createdAt time.Time
		if err := rows.Scan(&variantID, &sku, &name, &quantity, &priceAmount, &priceCurrency, &createdAt); err != nil {
			return nil, fmt.Errorf("order_repo: scan item: %w", err)
		}
		unitPrice, err := shared.NewMoney(priceAmount, priceCurrency)
		if err != nil {
			return nil, fmt.Errorf("order_repo: item money: %w", err)
		}
		// Construct Item directly — see loadItemsBatch comment for rationale.
		items = append(items, order.Item{
			VariantID: variantID,
			SKU:       sku,
			Name:      name,
			Quantity:  quantity,
			UnitPrice: unitPrice,
			CreatedAt: createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("order_repo: items rows: %w", err)
	}
	return items, nil
}
