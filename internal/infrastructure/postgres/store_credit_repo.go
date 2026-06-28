package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/storecredit"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/lib/pq"
)

// StoreCreditRepo implements storecredit.Repository.
type StoreCreditRepo struct {
	db *sql.DB
}

// NewStoreCreditRepo creates a StoreCreditRepo.
func NewStoreCreditRepo(db *sql.DB) (*StoreCreditRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("store_credit_repo: nil db")
	}
	return &StoreCreditRepo{db: db}, nil
}

func (r *StoreCreditRepo) GetBalance(ctx context.Context, customerID, currency string) (shared.Money, error) {
	customerID = strings.TrimSpace(customerID)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if customerID == "" || currency == "" {
		return shared.Money{}, fmt.Errorf("store_credit_repo: customer id and currency required")
	}
	const q = `SELECT balance FROM store_credit_accounts WHERE customer_id = $1 AND currency = $2`
	var balance int64
	err := r.db.QueryRowContext(ctx, q, customerID, currency).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return shared.Zero(currency)
	}
	if err != nil {
		return shared.Money{}, fmt.Errorf("store_credit_repo: get balance: %w", err)
	}
	return shared.NewMoney(balance, currency)
}

func (r *StoreCreditRepo) Issue(ctx context.Context, customerID string, amount shared.Money, note string) error {
	entry, err := storecredit.NewIssueEntry(id.New(), customerID, amount, note)
	if err != nil {
		return err
	}
	return r.applyEntry(ctx, entry, amount.Amount())
}

func (r *StoreCreditRepo) Redeem(ctx context.Context, customerID, orderID string, amount shared.Money) error {
	entry, err := storecredit.NewRedeemEntry(id.New(), customerID, orderID, amount)
	if err != nil {
		return err
	}
	return r.applyEntry(ctx, entry, -amount.Amount())
}

func (r *StoreCreditRepo) applyEntry(ctx context.Context, entry storecredit.Entry, delta int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store_credit_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	const ensureAccount = `INSERT INTO store_credit_accounts (customer_id, currency, balance, updated_at)
		VALUES ($1, $2, 0, now())
		ON CONFLICT (customer_id, currency) DO NOTHING`
	if _, err := tx.ExecContext(ctx, ensureAccount, entry.CustomerID, entry.Currency); err != nil {
		return fmt.Errorf("store_credit_repo: ensure account: %w", err)
	}

	var balance int64
	err = tx.QueryRowContext(ctx,
		`SELECT balance FROM store_credit_accounts WHERE customer_id = $1 AND currency = $2 FOR UPDATE`,
		entry.CustomerID, entry.Currency,
	).Scan(&balance)
	if err != nil {
		return fmt.Errorf("store_credit_repo: lock account: %w", err)
	}
	if balance+delta < 0 {
		return fmt.Errorf("%w", storecredit.ErrInsufficientBalance)
	}
	newBalance := balance + delta

	const upsertAccount = `INSERT INTO store_credit_accounts (customer_id, currency, balance, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (customer_id, currency) DO UPDATE
		SET balance = EXCLUDED.balance,
		    updated_at = now()`
	if _, err := tx.ExecContext(ctx, upsertAccount, entry.CustomerID, entry.Currency, newBalance); err != nil {
		return fmt.Errorf("store_credit_repo: upsert account: %w", err)
	}

	const insertLedger = `INSERT INTO store_credit_ledger (id, customer_id, currency, amount, kind, order_id, note, created_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)`
	var orderID interface{}
	if entry.OrderID != "" {
		orderID = entry.OrderID
	}
	_, err = tx.ExecContext(ctx, insertLedger,
		entry.ID, entry.CustomerID, entry.Currency, entry.Amount.Amount(), string(entry.Kind),
		orderID, entry.Note, entry.CreatedAt,
	)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23503" {
			return fmt.Errorf("store_credit_repo: customer or order not found")
		}
		return fmt.Errorf("store_credit_repo: insert ledger: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store_credit_repo: commit: %w", err)
	}
	return nil
}

func (r *StoreCreditRepo) ListLedger(ctx context.Context, customerID, currency string, offset, limit int) ([]storecredit.Entry, error) {
	customerID = strings.TrimSpace(customerID)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if customerID == "" {
		return nil, fmt.Errorf("store_credit_repo: customer id required")
	}
	if offset < 0 {
		return nil, fmt.Errorf("store_credit_repo: offset must be >= 0")
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	const q = `SELECT id, customer_id, currency, amount, kind, COALESCE(order_id::text, ''), note, created_at
		FROM store_credit_ledger
		WHERE customer_id = $1 AND ($2 = '' OR currency = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4`
	rows, err := r.db.QueryContext(ctx, q, customerID, currency, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store_credit_repo: list ledger: %w", err)
	}
	defer rows.Close()

	var entries []storecredit.Entry
	for rows.Next() {
		var e storecredit.Entry
		var amount int64
		var cur string
		var kind string
		if err := rows.Scan(&e.ID, &e.CustomerID, &cur, &amount, &kind, &e.OrderID, &e.Note, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store_credit_repo: scan: %w", err)
		}
		money, err := shared.NewMoney(amount, cur)
		if err != nil {
			return nil, fmt.Errorf("store_credit_repo: reconstruct money: %w", err)
		}
		e.Amount = money
		e.Currency = cur
		e.Kind = storecredit.Kind(kind)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store_credit_repo: rows: %w", err)
	}
	return entries, nil
}

var _ storecredit.Repository = (*StoreCreditRepo)(nil)
