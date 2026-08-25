package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/storecredit"
	"github.com/akarso/shopanda/internal/platform/apperror"
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

func (r *StoreCreditRepo) Issue(ctx context.Context, customerID string, amount shared.Money, note, idempotencyKey string) error {
	entry, err := storecredit.NewIssueEntry(id.New(), customerID, amount, note, idempotencyKey)
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

// storeCreditLedgerIdempotencyConstraint is the partial unique index name
// from migration 064 — used to distinguish an idempotent replay (safe to
// treat as success) from any other unique violation on this table.
const storeCreditLedgerIdempotencyConstraint = "idx_store_credit_ledger_idempotency"

func (r *StoreCreditRepo) applyEntry(ctx context.Context, entry storecredit.Entry, delta int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store_credit_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	if entry.IdempotencyKey != "" {
		var existingAmount int64
		var existingCurrency, existingNote string
		err := tx.QueryRowContext(ctx,
			`SELECT amount, currency, note FROM store_credit_ledger WHERE customer_id = $1 AND idempotency_key = $2`,
			entry.CustomerID, entry.IdempotencyKey,
		).Scan(&existingAmount, &existingCurrency, &existingNote)
		if err == nil {
			// A genuine replay (same key, same amount/currency/note) is a
			// no-op success — this is what makes a retried request safe.
			// A key reused for a DIFFERENT amount, currency, or note is
			// not a retry, it's a collision that would otherwise silently
			// apply the wrong value (or none at all) instead of the one
			// the caller actually asked for — surfaced as a conflict
			// rather than masked as success. No commit needed either
			// way; the deferred Rollback on this read-only check is a
			// no-op.
			if existingAmount != entry.Amount.Amount() || existingCurrency != entry.Currency || existingNote != entry.Note {
				return apperror.Conflict(fmt.Sprintf(
					"store credit: idempotency key %q already used for %d %s (note %q), not %d %s (note %q)",
					entry.IdempotencyKey, existingAmount, existingCurrency, existingNote, entry.Amount.Amount(), entry.Currency, entry.Note,
				))
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("store_credit_repo: check idempotency: %w", err)
		}
	}

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
	// Checked before computing the sum, not after: balance+delta on two
	// int64s can wrap around silently, which would either let an
	// overflowing issue through with a corrupted (possibly negative, then
	// "insufficient balance"-rejected, then wrapped positive again on a
	// retry) balance, or mask a real overflow as a false insufficient-
	// balance error.
	if delta > 0 && balance > math.MaxInt64-delta {
		return fmt.Errorf("store_credit_repo: balance overflow: %d + %d exceeds int64 range", balance, delta)
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

	const insertLedger = `INSERT INTO store_credit_ledger (id, customer_id, currency, amount, kind, order_id, note, idempotency_key, created_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, NULLIF($8, ''), $9)`
	var orderID interface{}
	if entry.OrderID != "" {
		orderID = entry.OrderID
	}
	_, err = tx.ExecContext(ctx, insertLedger,
		entry.ID, entry.CustomerID, entry.Currency, entry.Amount.Amount(), string(entry.Kind),
		orderID, entry.Note, entry.IdempotencyKey, entry.CreatedAt,
	)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			switch {
			case pqErr.Code == "23503":
				// Foreign key violation (unknown customer or order) is a
				// caller mistake, not a server fault — map to a 4xx-mapped
				// apperror instead of a bare error that JSONError would
				// otherwise default to 500 for.
				return apperror.NotFound("store credit: customer or order not found")
			case pqErr.Code == "23505" && pqErr.Constraint == storeCreditLedgerIdempotencyConstraint:
				// Lost the race with a concurrent identical request. Check
				// the winner's amount/currency before treating this as a
				// no-op success, same as the pre-check above — a race is
				// not exempt from the same-key-different-value conflict
				// that check exists to catch. Postgres aborts the rest of
				// a transaction after a statement error, so this must
				// query via r.db (a fresh connection), not tx — the
				// winner's row is already committed by the other
				// transaction, which is exactly why this one just failed.
				var winnerAmount int64
				var winnerCurrency, winnerNote string
				scanErr := r.db.QueryRowContext(ctx,
					`SELECT amount, currency, note FROM store_credit_ledger WHERE customer_id = $1 AND idempotency_key = $2`,
					entry.CustomerID, entry.IdempotencyKey,
				).Scan(&winnerAmount, &winnerCurrency, &winnerNote)
				if scanErr != nil {
					return fmt.Errorf("store_credit_repo: check idempotency race winner: %w", scanErr)
				}
				if winnerAmount != entry.Amount.Amount() || winnerCurrency != entry.Currency || winnerNote != entry.Note {
					return apperror.Conflict(fmt.Sprintf(
						"store credit: idempotency key %q already used for %d %s (note %q), not %d %s (note %q)",
						entry.IdempotencyKey, winnerAmount, winnerCurrency, winnerNote, entry.Amount.Amount(), entry.Currency, entry.Note,
					))
				}
				return nil
			}
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
