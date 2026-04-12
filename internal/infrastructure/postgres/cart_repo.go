package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// Compile-time check that CartRepo implements cart.CartRepository.
var _ cart.CartRepository = (*CartRepo)(nil)

// CartRepo implements cart.CartRepository using PostgreSQL.
type CartRepo struct {
	db *sql.DB
}

// NewCartRepo returns a new CartRepo backed by db.
func NewCartRepo(db *sql.DB) (*CartRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewCartRepo: nil *sql.DB")
	}
	return &CartRepo{db: db}, nil
}

// querier abstracts *sql.DB and *sql.Tx for read methods.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

// FindByID returns a cart with its items by ID.
// Returns (nil, nil) when not found.
// Uses a REPEATABLE READ read-only transaction for a consistent snapshot.
func (r *CartRepo) FindByID(ctx context.Context, id string) (*cart.Cart, error) {
	if id == "" {
		return nil, fmt.Errorf("cart_repo: find: empty id")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("cart_repo: find by id: begin tx: %w", err)
	}
	defer tx.Rollback()

	const q = `SELECT id, customer_id, status, currency, coupon_code, version, created_at, updated_at
		FROM carts WHERE id = $1`
	c, err := scanCart(tx.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cart_repo: find by id: %w", err)
	}
	items, err := loadItems(ctx, tx, c.ID)
	if err != nil {
		return nil, err
	}
	c.Items = items
	return c, nil
}

// FindActiveByCustomerID returns the active cart for a customer.
// Returns (nil, nil) when not found.
// Uses a REPEATABLE READ read-only transaction for a consistent snapshot.
func (r *CartRepo) FindActiveByCustomerID(ctx context.Context, customerID string) (*cart.Cart, error) {
	if customerID == "" {
		return nil, fmt.Errorf("cart_repo: find active: empty customer id")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("cart_repo: find active: begin tx: %w", err)
	}
	defer tx.Rollback()

	const q = `SELECT id, customer_id, status, currency, coupon_code, version, created_at, updated_at
		FROM carts WHERE customer_id = $1 AND status = 'active'
		LIMIT 1`
	c, err := scanCart(tx.QueryRowContext(ctx, q, customerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cart_repo: find active by customer: %w", err)
	}
	items, err := loadItems(ctx, tx, c.ID)
	if err != nil {
		return nil, err
	}
	c.Items = items
	return c, nil
}

// Save persists a cart and its items (upsert). Uses a transaction to ensure
// the cart header and items are written atomically.
// Optimistic locking: on update the version must match the value loaded by
// FindByID. If another writer incremented it first, Save returns a conflict error.
func (r *CartRepo) Save(ctx context.Context, c *cart.Cart) error {
	if c == nil {
		return fmt.Errorf("cart_repo: save: cart must not be nil")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cart_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Upsert cart header with optimistic lock.
	// INSERT: new cart, version starts at 1.
	// UPDATE: only succeeds when version matches; bumps version atomically.
	const upsertCart = `INSERT INTO carts (id, customer_id, status, currency, coupon_code, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			customer_id = EXCLUDED.customer_id,
			status = EXCLUDED.status,
			currency = EXCLUDED.currency,
			coupon_code = EXCLUDED.coupon_code,
			version = carts.version + 1,
			updated_at = EXCLUDED.updated_at
		WHERE carts.version = EXCLUDED.version
		RETURNING version`

	var customerID interface{}
	if c.CustomerID != "" {
		customerID = c.CustomerID
	}
	var couponCode interface{}
	if c.CouponCode != "" {
		couponCode = c.CouponCode
	}

	var newVersion int
	err = tx.QueryRowContext(ctx, upsertCart,
		c.ID, customerID, string(c.Status()), c.Currency, couponCode,
		c.Version, c.CreatedAt, c.UpdatedAt,
	).Scan(&newVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.Conflict("cart was modified concurrently")
	}
	if err != nil {
		return fmt.Errorf("cart_repo: upsert cart: %w", err)
	}

	// Replace items: delete all, re-insert.
	const deleteItems = `DELETE FROM cart_items WHERE cart_id = $1`
	if _, err := tx.ExecContext(ctx, deleteItems, c.ID); err != nil {
		return fmt.Errorf("cart_repo: delete items: %w", err)
	}

	if len(c.Items) > 0 {
		const insertItem = `INSERT INTO cart_items (cart_id, variant_id, quantity, unit_price, currency, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`
		for i := range c.Items {
			item := &c.Items[i]
			_, err = tx.ExecContext(ctx, insertItem,
				c.ID, item.VariantID, item.Quantity,
				item.UnitPrice.Amount(), item.UnitPrice.Currency(),
				item.CreatedAt, item.UpdatedAt,
			)
			if err != nil {
				return fmt.Errorf("cart_repo: insert item %q: %w", item.VariantID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("cart_repo: commit: %w", err)
	}
	c.Version = newVersion
	return nil
}

// Delete removes a cart and its items by ID (CASCADE handles items).
func (r *CartRepo) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("cart_repo: delete: empty id")
	}
	const q = `DELETE FROM carts WHERE id = $1`
	_, err := r.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("cart_repo: delete: %w", err)
	}
	return nil
}

// loadItems fetches all items for a cart, ordered by created_at.
func loadItems(ctx context.Context, q querier, cartID string) ([]cart.Item, error) {
	const query = `SELECT variant_id, quantity, unit_price, currency, created_at, updated_at
		FROM cart_items WHERE cart_id = $1
		ORDER BY created_at`
	rows, err := q.QueryContext(ctx, query, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart_repo: load items: %w", err)
	}
	defer rows.Close()

	var items []cart.Item
	for rows.Next() {
		var variantID string
		var quantity int
		var priceAmount int64
		var priceCurrency string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&variantID, &quantity, &priceAmount, &priceCurrency, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("cart_repo: scan item: %w", err)
		}
		unitPrice, err := shared.NewMoney(priceAmount, priceCurrency)
		if err != nil {
			return nil, fmt.Errorf("cart_repo: item money: %w", err)
		}
		items = append(items, cart.Item{
			VariantID: variantID,
			Quantity:  quantity,
			UnitPrice: unitPrice,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cart_repo: items rows: %w", err)
	}
	return items, nil
}

// scanCart reads a cart header from a row scanner.
func scanCart(row *sql.Row) (*cart.Cart, error) {
	var c cart.Cart
	var customerID sql.NullString
	var couponCode sql.NullString
	var status string
	err := row.Scan(&c.ID, &customerID, &status, &c.Currency, &couponCode, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if customerID.Valid {
		c.CustomerID = customerID.String
	}
	if couponCode.Valid {
		c.CouponCode = couponCode.String
	}
	// Reconstruct the cart with proper status via SetStatusFromDB.
	if err := c.SetStatusFromDB(cart.CartStatus(status)); err != nil {
		return nil, fmt.Errorf("cart_repo: invalid status %q: %w", status, err)
	}
	return &c, nil
}
