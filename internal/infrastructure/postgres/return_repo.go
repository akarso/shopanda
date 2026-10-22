package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
	"github.com/akarso/shopanda/internal/domain/shared"
)

var _ domainReturns.Repository = (*ReturnRepo)(nil)

// ReturnRepo implements returns.Repository using PostgreSQL.
type ReturnRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// NewReturnRepo returns a new ReturnRepo backed by db.
func NewReturnRepo(db *sql.DB) (*ReturnRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewReturnRepo: nil *sql.DB")
	}
	return &ReturnRepo{db: db}, nil
}

// WithTx returns a repo whose Update, LockForRestock, and UpdateIfUnrestocked
// run on tx. LockForRestock also loads return items on that same tx. Save
// still opens its own transaction on the pool. Receive binds this so the
// return row lock, MarkReceived, restock, and RestockedAt commit together.
func (r *ReturnRepo) WithTx(tx *sql.Tx) domainReturns.Repository {
	return &ReturnRepo{db: r.db, tx: tx}
}

// LockForRestock locks the return row for the caller's transaction and
// returns the current header plus items. RestockedAt on the result is the
// committed value, so a concurrent Receive that already finished is visible
// before this transaction adds stock. The caller must be inside a transaction.
func (r *ReturnRepo) LockForRestock(ctx context.Context, id string) (*domainReturns.Return, error) {
	if id == "" {
		return nil, fmt.Errorf("return_repo: lock: empty id")
	}
	if r.tx == nil {
		return nil, fmt.Errorf("return_repo: lock: transaction required")
	}
	q := `SELECT ` + returnCols + ` FROM order_returns WHERE id = $1 FOR UPDATE`
	ret, err := hydrateReturn(r.tx.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("return_repo: lock: %w", err)
	}
	items, err := r.loadItems(ctx, ret.ID)
	if err != nil {
		return nil, err
	}
	if err := ret.SetItemsFromDB(items); err != nil {
		return nil, fmt.Errorf("return_repo: set items: %w", err)
	}
	return ret, nil
}

type returnScanner interface {
	Scan(dest ...interface{}) error
}

const returnCols = `id, order_id, customer_id, reason, status, currency,
	restocked_at, refunded_at, created_at, updated_at`

func hydrateReturn(s returnScanner) (*domainReturns.Return, error) {
	var ret domainReturns.Return
	var status string
	var restockedAt, refundedAt sql.NullTime
	err := s.Scan(
		&ret.ID, &ret.OrderID, &ret.CustomerID, &ret.Reason, &status, &ret.Currency,
		&restockedAt, &refundedAt, &ret.CreatedAt, &ret.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := ret.SetStatusFromDB(status); err != nil {
		return nil, err
	}
	if restockedAt.Valid {
		t := restockedAt.Time
		ret.RestockedAt = &t
	}
	if refundedAt.Valid {
		t := refundedAt.Time
		ret.RefundedAt = &t
	}
	return &ret, nil
}

// Save inserts a new return and its items.
func (r *ReturnRepo) Save(ctx context.Context, ret *domainReturns.Return) error {
	if ret == nil {
		return fmt.Errorf("return_repo: save: return must not be nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("return_repo: save begin: %w", err)
	}
	defer tx.Rollback()

	const headerQ = `INSERT INTO order_returns
		(id, order_id, customer_id, reason, status, currency, restocked_at, refunded_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err = tx.ExecContext(ctx, headerQ,
		ret.ID, ret.OrderID, ret.CustomerID, ret.Reason, string(ret.Status()), ret.Currency,
		nullTime(ret.RestockedAt), nullTime(ret.RefundedAt), ret.CreatedAt, ret.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("return_repo: save header: %w", err)
	}

	const itemQ = `INSERT INTO order_return_items
		(return_id, variant_id, sku, name, quantity, unit_price, currency, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	for _, item := range ret.Items() {
		_, err = tx.ExecContext(ctx, itemQ,
			ret.ID, item.VariantID, item.SKU, item.Name, item.Quantity,
			item.UnitPrice.Amount(), item.UnitPrice.Currency(), item.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("return_repo: save item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("return_repo: save commit: %w", err)
	}
	return nil
}

// FindByID returns a return with its items.
func (r *ReturnRepo) FindByID(ctx context.Context, id string) (*domainReturns.Return, error) {
	if id == "" {
		return nil, fmt.Errorf("return_repo: find: empty id")
	}
	q := `SELECT ` + returnCols + ` FROM order_returns WHERE id = $1`
	ret, err := hydrateReturn(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("return_repo: find by id: %w", err)
	}
	items, err := r.loadItems(ctx, ret.ID)
	if err != nil {
		return nil, err
	}
	if err := ret.SetItemsFromDB(items); err != nil {
		return nil, fmt.Errorf("return_repo: set items: %w", err)
	}
	return ret, nil
}

// FindByCustomerID returns all returns for a customer, newest first.
func (r *ReturnRepo) FindByCustomerID(ctx context.Context, customerID string) ([]domainReturns.Return, error) {
	if customerID == "" {
		return nil, fmt.Errorf("return_repo: find by customer: empty customer id")
	}
	q := `SELECT ` + returnCols + ` FROM order_returns WHERE customer_id = $1 ORDER BY created_at DESC`
	return r.queryReturnsWithItems(ctx, q, customerID)
}

// List returns returns ordered by created_at desc with pagination.
func (r *ReturnRepo) List(ctx context.Context, offset, limit int) ([]domainReturns.Return, error) {
	if offset < 0 {
		return nil, fmt.Errorf("return_repo: list: negative offset")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("return_repo: list: limit must be positive")
	}
	q := `SELECT ` + returnCols + ` FROM order_returns ORDER BY created_at DESC OFFSET $1 LIMIT $2`
	return r.queryReturnsWithItems(ctx, q, offset, limit)
}

// FindByOrderID returns all returns for an order, newest first.
func (r *ReturnRepo) FindByOrderID(ctx context.Context, orderID string) ([]domainReturns.Return, error) {
	if orderID == "" {
		return nil, fmt.Errorf("return_repo: find by order: empty order id")
	}
	q := `SELECT ` + returnCols + ` FROM order_returns WHERE order_id = $1 ORDER BY created_at DESC`
	return r.queryReturnsWithItems(ctx, q, orderID)
}

func (r *ReturnRepo) queryReturnsWithItems(ctx context.Context, q string, args ...interface{}) ([]domainReturns.Return, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("return_repo: query: %w", err)
	}
	defer rows.Close()

	var list []domainReturns.Return
	var ids []string
	for rows.Next() {
		ret, err := hydrateReturn(rows)
		if err != nil {
			return nil, fmt.Errorf("return_repo: scan: %w", err)
		}
		list = append(list, *ret)
		ids = append(ids, ret.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("return_repo: rows: %w", err)
	}
	if len(list) == 0 {
		return list, nil
	}

	itemMap, err := r.loadItemsBatch(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range list {
		items := itemMap[list[i].ID]
		if err := list[i].SetItemsFromDB(items); err != nil {
			return nil, fmt.Errorf("return_repo: set items: %w", err)
		}
	}
	return list, nil
}

// Update persists a status transition only while the row is still in
// StoredStatus. restocked_at and refunded_at keep a value that is already
// stored, so a stale transition cannot clear them.
func (r *ReturnRepo) Update(ctx context.Context, ret *domainReturns.Return) error {
	if ret == nil {
		return fmt.Errorf("return_repo: update: return must not be nil")
	}
	const q = `UPDATE order_returns
		SET status = $1,
			restocked_at = COALESCE(restocked_at, $2),
			refunded_at = COALESCE(refunded_at, $3),
			updated_at = $4
		WHERE id = $5 AND status = $6`
	result, err := r.exec(ctx, q,
		string(ret.Status()), nullTime(ret.RestockedAt), nullTime(ret.RefundedAt), ret.UpdatedAt, ret.ID, string(ret.StoredStatus()),
	)
	if err != nil {
		return fmt.Errorf("return_repo: update: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("return_repo: update rows: %w", err)
	}
	if n == 0 {
		var one int
		err := r.queryRow(ctx, `SELECT 1 FROM order_returns WHERE id = $1`, ret.ID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("return_repo: update: not found")
		}
		if err != nil {
			return fmt.Errorf("return_repo: update status: %w", err)
		}
		return domainReturns.ErrStatusConflict
	}
	ret.AcceptStoredStatus()
	return nil
}

// UpdateIfUnrestocked persists status and restocked_at only while
// restocked_at is still null. Zero rows means another writer already
// recorded the restock; the caller rolls back and returns success.
func (r *ReturnRepo) UpdateIfUnrestocked(ctx context.Context, ret *domainReturns.Return) error {
	if ret == nil {
		return fmt.Errorf("return_repo: update restock: return must not be nil")
	}
	if r.tx == nil {
		return fmt.Errorf("return_repo: update restock: transaction required")
	}
	const q = `UPDATE order_returns
		SET status = $1,
			restocked_at = COALESCE(restocked_at, $2),
			refunded_at = COALESCE(refunded_at, $3),
			updated_at = $4
		WHERE id = $5 AND restocked_at IS NULL`
	result, err := r.tx.ExecContext(ctx, q,
		string(ret.Status()), nullTime(ret.RestockedAt), nullTime(ret.RefundedAt), ret.UpdatedAt, ret.ID,
	)
	if err != nil {
		return fmt.Errorf("return_repo: update restock: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("return_repo: update restock rows: %w", err)
	}
	if n == 0 {
		return domainReturns.ErrAlreadyRestocked
	}
	return nil
}

func (r *ReturnRepo) loadItems(ctx context.Context, returnID string) ([]domainReturns.Item, error) {
	m, err := r.loadItemsBatch(ctx, []string{returnID})
	if err != nil {
		return nil, err
	}
	return m[returnID], nil
}

func (r *ReturnRepo) loadItemsBatch(ctx context.Context, returnIDs []string) (map[string][]domainReturns.Item, error) {
	const q = `SELECT return_id, variant_id, sku, name, quantity, unit_price, currency, created_at
		FROM order_return_items WHERE return_id = ANY($1)
		ORDER BY return_id, variant_id`
	rows, err := r.query(ctx, q, returnIDs)
	if err != nil {
		return nil, fmt.Errorf("return_repo: load items: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]domainReturns.Item, len(returnIDs))
	for rows.Next() {
		var returnID, variantID, sku, name, currency string
		var qty int
		var amount int64
		var createdAt time.Time
		if err := rows.Scan(&returnID, &variantID, &sku, &name, &qty, &amount, &currency, &createdAt); err != nil {
			return nil, fmt.Errorf("return_repo: scan item: %w", err)
		}
		money, err := shared.NewMoney(amount, currency)
		if err != nil {
			return nil, fmt.Errorf("return_repo: item money: %w", err)
		}
		out[returnID] = append(out[returnID], domainReturns.Item{
			VariantID: variantID,
			SKU:       sku,
			Name:      name,
			Quantity:  qty,
			UnitPrice: money,
			CreatedAt: createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("return_repo: item rows: %w", err)
	}
	return out, nil
}

func (r *ReturnRepo) queryRow(ctx context.Context, q string, args ...interface{}) *sql.Row {
	if r.tx != nil {
		return r.tx.QueryRowContext(ctx, q, args...)
	}
	return r.db.QueryRowContext(ctx, q, args...)
}

func (r *ReturnRepo) query(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	if r.tx != nil {
		return r.tx.QueryContext(ctx, q, args...)
	}
	return r.db.QueryContext(ctx, q, args...)
}

func (r *ReturnRepo) exec(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	if r.tx != nil {
		return r.tx.ExecContext(ctx, q, args...)
	}
	return r.db.ExecContext(ctx, q, args...)
}

func nullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}
