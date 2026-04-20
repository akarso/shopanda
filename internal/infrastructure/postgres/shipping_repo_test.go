package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewShipment(t *testing.T, orderID string) shipping.Shipment {
	t.Helper()
	cost := shared.MustNewMoney(500, "EUR")
	s, err := shipping.NewShipment(id.New(), orderID, shipping.MethodFlatRate, cost)
	if err != nil {
		t.Fatalf("NewShipment: %v", err)
	}
	return s
}

func TestShippingRepo_NilDB(t *testing.T) {
	_, err := postgres.NewShippingRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestShippingRepo_CreateAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM shipments") })

	repo, err := postgres.NewShippingRepo(db)
	if err != nil {
		t.Fatalf("NewShippingRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewShipment(t, "order-ship-1")
	if err := repo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.ID != s.ID {
		t.Fatalf("ID = %q, want %q", got.ID, s.ID)
	}
	if got.OrderID != "order-ship-1" {
		t.Fatalf("OrderID = %q, want %q", got.OrderID, "order-ship-1")
	}
	if got.Status() != shipping.StatusPending {
		t.Fatalf("Status = %q, want %q", got.Status(), shipping.StatusPending)
	}
	if got.Cost.Amount() != 500 {
		t.Fatalf("Cost = %d, want 500", got.Cost.Amount())
	}
}

func TestShippingRepo_FindByOrderID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM shipments") })

	repo, err := postgres.NewShippingRepo(db)
	if err != nil {
		t.Fatalf("NewShippingRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewShipment(t, "order-ship-2")
	if err := repo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByOrderID(ctx, "order-ship-2")
	if err != nil {
		t.Fatalf("FindByOrderID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByOrderID returned nil")
	}
	if got.ID != s.ID {
		t.Fatalf("ID = %q, want %q", got.ID, s.ID)
	}
}

func TestShippingRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewShippingRepo(db)
	if err != nil {
		t.Fatalf("NewShippingRepo: %v", err)
	}
	got, err := repo.FindByID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for nonexistent shipment")
	}
}

func TestShippingRepo_FindByID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewShippingRepo(db)
	if err != nil {
		t.Fatalf("NewShippingRepo: %v", err)
	}
	_, err = repo.FindByID(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestShippingRepo_CreateDuplicate(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM shipments") })

	repo, err := postgres.NewShippingRepo(db)
	if err != nil {
		t.Fatalf("NewShippingRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewShipment(t, "order-ship-dup")
	if err := repo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	s2 := mustNewShipment(t, "order-ship-dup")
	err = repo.Create(ctx, &s2)
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestShippingRepo_UpdateStatus(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM shipments") })

	repo, err := postgres.NewShippingRepo(db)
	if err != nil {
		t.Fatalf("NewShippingRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewShipment(t, "order-ship-upd")
	if err := repo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	prevUpdatedAt := s.UpdatedAt
	if err := s.Ship("TRACK-001", "provider-ref-1"); err != nil {
		t.Fatalf("Ship: %v", err)
	}

	if err := repo.UpdateStatus(ctx, &s, prevUpdatedAt); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := repo.FindByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("FindByID after update: %v", err)
	}
	if got.Status() != shipping.StatusShipped {
		t.Fatalf("Status = %q, want %q", got.Status(), shipping.StatusShipped)
	}
	if got.TrackingNumber != "TRACK-001" {
		t.Fatalf("TrackingNumber = %q, want %q", got.TrackingNumber, "TRACK-001")
	}
	if got.ProviderRef != "provider-ref-1" {
		t.Fatalf("ProviderRef = %q, want %q", got.ProviderRef, "provider-ref-1")
	}
}

func TestShippingRepo_UpdateStatus_OptimisticLock(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM shipments") })

	repo, err := postgres.NewShippingRepo(db)
	if err != nil {
		t.Fatalf("NewShippingRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewShipment(t, "order-ship-lock")
	if err := repo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	stale := s.UpdatedAt.Add(-time.Second)
	if err := s.Ship("TRACK-X", "ref-x"); err != nil {
		t.Fatalf("Ship: %v", err)
	}

	err = repo.UpdateStatus(ctx, &s, stale)
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("expected conflict error for stale timestamp, got %v", err)
	}
}
