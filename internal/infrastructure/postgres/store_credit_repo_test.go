package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func seedStoreCreditCustomer(t *testing.T, db *sql.DB) string {
	t.Helper()
	customerRepo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	c := mustNewCustomer(t, id.New()+"@example.com")
	if err := customerRepo.Create(context.Background(), &c); err != nil {
		t.Fatalf("Create customer: %v", err)
	}
	return c.ID
}

func TestStoreCreditRepo_Issue_IdempotencyKeyPreventsDoubleCredit(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM store_credit_ledger")
		db.Exec("DELETE FROM store_credit_accounts")
		db.Exec("DELETE FROM customers")
	})

	repo, err := postgres.NewStoreCreditRepo(db)
	if err != nil {
		t.Fatalf("NewStoreCreditRepo: %v", err)
	}
	customerID := seedStoreCreditCustomer(t, db)
	ctx := context.Background()
	amount, err := shared.NewMoney(1000, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}

	if err := repo.Issue(ctx, customerID, amount, "goodwill", "retry-key-1"); err != nil {
		t.Fatalf("first Issue: %v", err)
	}
	if err := repo.Issue(ctx, customerID, amount, "goodwill", "retry-key-1"); err != nil {
		t.Fatalf("second Issue (same key) should be a no-op, not an error: %v", err)
	}

	balance, err := repo.GetBalance(ctx, customerID, "EUR")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.Amount() != 1000 {
		t.Fatalf("balance = %d, want 1000 (must not double-credit on retried idempotency key)", balance.Amount())
	}
}

func TestStoreCreditRepo_Issue_DifferentIdempotencyKeysBothCredit(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM store_credit_ledger")
		db.Exec("DELETE FROM store_credit_accounts")
		db.Exec("DELETE FROM customers")
	})

	repo, err := postgres.NewStoreCreditRepo(db)
	if err != nil {
		t.Fatalf("NewStoreCreditRepo: %v", err)
	}
	customerID := seedStoreCreditCustomer(t, db)
	ctx := context.Background()
	amount, err := shared.NewMoney(500, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}

	if err := repo.Issue(ctx, customerID, amount, "note-a", "key-a"); err != nil {
		t.Fatalf("Issue key-a: %v", err)
	}
	if err := repo.Issue(ctx, customerID, amount, "note-b", "key-b"); err != nil {
		t.Fatalf("Issue key-b: %v", err)
	}

	balance, err := repo.GetBalance(ctx, customerID, "EUR")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.Amount() != 1000 {
		t.Fatalf("balance = %d, want 1000 (distinct keys must both credit)", balance.Amount())
	}
}

func TestStoreCreditRepo_Issue_NoKeyAllowsRepeatedCredits(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM store_credit_ledger")
		db.Exec("DELETE FROM store_credit_accounts")
		db.Exec("DELETE FROM customers")
	})

	repo, err := postgres.NewStoreCreditRepo(db)
	if err != nil {
		t.Fatalf("NewStoreCreditRepo: %v", err)
	}
	customerID := seedStoreCreditCustomer(t, db)
	ctx := context.Background()
	amount, err := shared.NewMoney(500, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}

	// No idempotency key supplied (empty string): the old, unprotected
	// behavior is preserved for callers that don't opt in (e.g. Redeem's
	// rollback path at call sites that pass a per-order key of their own,
	// or any caller that passes none at all).
	if err := repo.Issue(ctx, customerID, amount, "note", ""); err != nil {
		t.Fatalf("first Issue: %v", err)
	}
	if err := repo.Issue(ctx, customerID, amount, "note", ""); err != nil {
		t.Fatalf("second Issue: %v", err)
	}

	balance, err := repo.GetBalance(ctx, customerID, "EUR")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.Amount() != 1000 {
		t.Fatalf("balance = %d, want 1000 (no key means no dedup)", balance.Amount())
	}
}
