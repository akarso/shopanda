package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
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

// FindByID returns an order with its items by ID.
// Returns (nil, nil) when not found.
func (r *OrderRepo) FindByID(ctx context.Context, id string) (*order.Order, error) {
	if id == "" {
		return nil, fmt.Errorf("order_repo: find: empty id")
	}
	const q = `SELECT id, customer_id, status, currency, total_amount, total_currency, created_at, updated_at
		FROM orders WHERE id = $1`
	o, err := r.scanOrder(r.db.QueryRowContext(ctx, q, id))
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
	o.Items = items
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
	for rows.Next() {
		o, err := r.scanOrderRow(rows)
		if err != nil {
			return nil, fmt.Errorf("order_repo: scan order: %w", err)
		}
		items, err := r.loadItems(ctx, o.ID)
		if err != nil {
			return nil, err
		}
		o.Items = items
		orders = append(orders, *o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("order_repo: rows: %w", err)
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

	if len(o.Items) > 0 {
		const insertItem = `INSERT INTO order_items (order_id, variant_id, sku, name, quantity, unit_price, currency, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
		for i := range o.Items {
			item := &o.Items[i]
			_, err = tx.ExecContext(ctx, insertItem,
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

// loadItems fetches all items for an order, ordered by created_at.
func (r *OrderRepo) loadItems(ctx context.Context, orderID string) ([]order.Item, error) {
	const q = `SELECT variant_id, sku, name, quantity, unit_price, currency, created_at
		FROM order_items WHERE order_id = $1
		ORDER BY created_at`
	rows, err := r.db.QueryContext(ctx, q, orderID)
	if err != nil {
		return nil, fmt.Errorf("order_repo: load items: %w", err)
	}
	defer rows.Close()

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

// scanOrder reads an order header from a single row.
func (r *OrderRepo) scanOrder(row *sql.Row) (*order.Order, error) {
	var o order.Order
	var status string
	var totalAmount int64
	var totalCurrency string
	err := row.Scan(&o.ID, &o.CustomerID, &status, &o.Currency,
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

// scanOrderRow reads an order header from a multi-row result set.
func (r *OrderRepo) scanOrderRow(rows *sql.Rows) (*order.Order, error) {
	var o order.Order
	var status string
	var totalAmount int64
	var totalCurrency string
	err := rows.Scan(&o.ID, &o.CustomerID, &status, &o.Currency,
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
