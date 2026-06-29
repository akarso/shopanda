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

const cartColumns = `id, customer_id, status, currency, coupon_code, merged_guest_id, version, created_at, updated_at, recovery_email_sent_at`

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

	const q = `SELECT ` + cartColumns + ` FROM carts WHERE id = $1`
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

	const q = `SELECT ` + cartColumns + ` FROM carts WHERE customer_id = $1 AND status = 'active'
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
	const upsertCart = `INSERT INTO carts (id, customer_id, status, currency, coupon_code, merged_guest_id, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			customer_id = EXCLUDED.customer_id,
			status = EXCLUDED.status,
			currency = EXCLUDED.currency,
			coupon_code = EXCLUDED.coupon_code,
			merged_guest_id = EXCLUDED.merged_guest_id,
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
	var mergedGuestID interface{}
	if c.MergedGuestID != "" {
		mergedGuestID = c.MergedGuestID
	}

	var newVersion int
	err = tx.QueryRowContext(ctx, upsertCart,
		c.ID, customerID, string(c.Status()), c.Currency, couponCode, mergedGuestID,
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

// FindRecoveryCandidates returns active customer carts with items that are stale and unemailed.
func (r *CartRepo) FindRecoveryCandidates(ctx context.Context, staleBefore time.Time, limit int) ([]*cart.Cart, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("cart_repo: find recovery candidates: limit must be positive")
	}
	const q = `
		SELECT ` + cartColumns + `
		FROM carts c
		WHERE c.status = 'active'
		  AND c.customer_id IS NOT NULL
		  AND c.recovery_email_sent_at IS NULL
		  AND c.updated_at <= $1
		  AND EXISTS (SELECT 1 FROM cart_items ci WHERE ci.cart_id = c.id)
		ORDER BY c.updated_at ASC
		LIMIT $2`
	rows, err := r.db.QueryContext(ctx, q, staleBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("cart_repo: find recovery candidates: %w", err)
	}
	defer rows.Close()

	var carts []*cart.Cart
	for rows.Next() {
		c, err := scanCartRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("cart_repo: find recovery candidates scan: %w", err)
		}
		items, err := loadItems(ctx, r.db, c.ID)
		if err != nil {
			return nil, err
		}
		c.Items = items
		carts = append(carts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cart_repo: find recovery candidates rows: %w", err)
	}
	return carts, nil
}

// MarkRecoveryEmailSent records a recovery email send when not already recorded.
func (r *CartRepo) MarkRecoveryEmailSent(ctx context.Context, cartID string, sentAt time.Time) (bool, error) {
	if cartID == "" {
		return false, fmt.Errorf("cart_repo: mark recovery email sent: empty id")
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE carts
		SET recovery_email_sent_at = $2
		WHERE id = $1 AND recovery_email_sent_at IS NULL`,
		cartID, sentAt,
	)
	if err != nil {
		return false, fmt.Errorf("cart_repo: mark recovery email sent: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("cart_repo: mark recovery email sent rows: %w", err)
	}
	return n == 1, nil
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
	return scanCartRow(row.Scan)
}

func scanCartRow(scan func(dest ...interface{}) error) (*cart.Cart, error) {
	var c cart.Cart
	var customerID sql.NullString
	var couponCode sql.NullString
	var mergedGuestID sql.NullString
	var recoveryEmailSentAt sql.NullTime
	var status string
	err := scan(&c.ID, &customerID, &status, &c.Currency, &couponCode, &mergedGuestID, &c.Version, &c.CreatedAt, &c.UpdatedAt, &recoveryEmailSentAt)
	if err != nil {
		return nil, err
	}
	if customerID.Valid {
		c.CustomerID = customerID.String
	}
	if couponCode.Valid {
		c.CouponCode = couponCode.String
	}
	if mergedGuestID.Valid {
		c.MergedGuestID = mergedGuestID.String
	}
	if recoveryEmailSentAt.Valid {
		sentAt := recoveryEmailSentAt.Time
		c.RecoveryEmailSentAt = &sentAt
	}
	// Reconstruct the cart with proper status via SetStatusFromDB.
	if err := c.SetStatusFromDB(cart.CartStatus(status)); err != nil {
		return nil, fmt.Errorf("cart_repo: invalid status %q: %w", status, err)
	}
	return &c, nil
}
