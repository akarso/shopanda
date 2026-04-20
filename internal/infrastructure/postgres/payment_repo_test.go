package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewPayment(t *testing.T, orderID string) payment.Payment {
	t.Helper()
	amount := shared.MustNewMoney(2500, "EUR")
	p, err := payment.NewPayment(id.New(), orderID, payment.MethodManual, amount)
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	return p
}

func TestPaymentRepo_NilDB(t *testing.T) {
	_, err := postgres.NewPaymentRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestPaymentRepo_CreateAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM payments") })

	repo, err := postgres.NewPaymentRepo(db)
	if err != nil {
		t.Fatalf("NewPaymentRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPayment(t, "order-pay-1")
	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.ID != p.ID {
		t.Fatalf("ID = %q, want %q", got.ID, p.ID)
	}
	if got.OrderID != p.OrderID {
		t.Fatalf("OrderID = %q, want %q", got.OrderID, p.OrderID)
	}
	if got.Status() != payment.StatusPending {
		t.Fatalf("Status = %q, want %q", got.Status(), payment.StatusPending)
	}
	if got.Amount.Amount() != 2500 {
		t.Fatalf("Amount = %d, want 2500", got.Amount.Amount())
	}
}

func TestPaymentRepo_FindByOrderID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM payments") })

	repo, err := postgres.NewPaymentRepo(db)
	if err != nil {
		t.Fatalf("NewPaymentRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPayment(t, "order-pay-2")
	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByOrderID(ctx, "order-pay-2")
	if err != nil {
		t.Fatalf("FindByOrderID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByOrderID returned nil")
	}
	if got.ID != p.ID {
		t.Fatalf("ID = %q, want %q", got.ID, p.ID)
	}
}

func TestPaymentRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPaymentRepo(db)
	if err != nil {
		t.Fatalf("NewPaymentRepo: %v", err)
	}
	ctx := context.Background()

	got, err := repo.FindByID(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for nonexistent payment")
	}
}

func TestPaymentRepo_FindByID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPaymentRepo(db)
	if err != nil {
		t.Fatalf("NewPaymentRepo: %v", err)
	}
	_, err = repo.FindByID(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestPaymentRepo_CreateDuplicate(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM payments") })

	repo, err := postgres.NewPaymentRepo(db)
	if err != nil {
		t.Fatalf("NewPaymentRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPayment(t, "order-pay-dup")
	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Second payment for same order → conflict.
	p2 := mustNewPayment(t, "order-pay-dup")
	err = repo.Create(ctx, &p2)
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestPaymentRepo_UpdateStatus(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM payments") })

	repo, err := postgres.NewPaymentRepo(db)
	if err != nil {
		t.Fatalf("NewPaymentRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPayment(t, "order-pay-upd")
	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	prevUpdatedAt := p.UpdatedAt
	if err := p.Complete("ref-123"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if err := repo.UpdateStatus(ctx, &p, prevUpdatedAt); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := repo.FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("FindByID after update: %v", err)
	}
	if got.Status() != payment.StatusCompleted {
		t.Fatalf("Status = %q, want %q", got.Status(), payment.StatusCompleted)
	}
	if got.ProviderRef != "ref-123" {
		t.Fatalf("ProviderRef = %q, want %q", got.ProviderRef, "ref-123")
	}
}

func TestPaymentRepo_UpdateStatus_OptimisticLock(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM payments") })

	repo, err := postgres.NewPaymentRepo(db)
	if err != nil {
		t.Fatalf("NewPaymentRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPayment(t, "order-pay-lock")
	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Use a stale timestamp → should get conflict.
	stale := p.UpdatedAt.Add(-time.Second)
	if err := p.Complete("ref-x"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	err = repo.UpdateStatus(ctx, &p, stale)
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("expected conflict error for stale timestamp, got %v", err)
	}
}
