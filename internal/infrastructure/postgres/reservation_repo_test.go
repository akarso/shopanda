package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func seedStock(t *testing.T, stockRepo *postgres.StockRepo, variantID string, qty int) {
	t.Helper()
	entry, err := inventory.NewStockEntry(variantID, qty)
	if err != nil {
		t.Fatalf("NewStockEntry: %v", err)
	}
	if err := stockRepo.SetStock(context.Background(), &entry); err != nil {
		t.Fatalf("SetStock: %v", err)
	}
}

func TestReservationRepo_Reserve(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM reservations")
		db.Exec("DELETE FROM stock")
	})

	vid := seedVariant(t, db)
	stockRepo := postgres.NewStockRepo(db)
	seedStock(t, stockRepo, vid, 10)

	repo := postgres.NewReservationRepo(db)
	res, err := inventory.NewReservation(id.New(), vid, 3, time.Now().Add(15*time.Minute))
	if err != nil {
		t.Fatalf("NewReservation: %v", err)
	}

	if err := repo.Reserve(context.Background(), &res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// Stock should be decremented.
	stock, err := stockRepo.GetStock(context.Background(), vid)
	if err != nil {
		t.Fatalf("GetStock: %v", err)
	}
	if stock.Quantity != 7 {
		t.Errorf("stock after reserve: got %d, want 7", stock.Quantity)
	}

	// Reservation should exist.
	found, err := repo.FindByID(context.Background(), res.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil {
		t.Fatal("expected reservation, got nil")
	}
	if found.Status != inventory.ReservationActive {
		t.Errorf("Status = %q, want %q", found.Status, inventory.ReservationActive)
	}
}

func TestReservationRepo_Reserve_InsufficientStock(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM reservations")
		db.Exec("DELETE FROM stock")
	})

	vid := seedVariant(t, db)
	stockRepo := postgres.NewStockRepo(db)
	seedStock(t, stockRepo, vid, 2)

	repo := postgres.NewReservationRepo(db)
	res, _ := inventory.NewReservation(id.New(), vid, 5, time.Now().Add(15*time.Minute))

	err := repo.Reserve(context.Background(), &res)
	if err == nil {
		t.Fatal("expected error for insufficient stock")
	}
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Errorf("expected conflict error, got: %v", err)
	}

	// Stock should be unchanged.
	stock, _ := stockRepo.GetStock(context.Background(), vid)
	if stock.Quantity != 2 {
		t.Errorf("stock after failed reserve: got %d, want 2", stock.Quantity)
	}
}

func TestReservationRepo_Release(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM reservations")
		db.Exec("DELETE FROM stock")
	})

	vid := seedVariant(t, db)
	stockRepo := postgres.NewStockRepo(db)
	seedStock(t, stockRepo, vid, 10)

	repo := postgres.NewReservationRepo(db)
	res, _ := inventory.NewReservation(id.New(), vid, 4, time.Now().Add(15*time.Minute))
	if err := repo.Reserve(context.Background(), &res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	if err := repo.Release(context.Background(), res.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Stock should be restored.
	stock, _ := stockRepo.GetStock(context.Background(), vid)
	if stock.Quantity != 10 {
		t.Errorf("stock after release: got %d, want 10", stock.Quantity)
	}

	// Reservation should be marked released.
	found, _ := repo.FindByID(context.Background(), res.ID)
	if found.Status != inventory.ReservationReleased {
		t.Errorf("Status = %q, want %q", found.Status, inventory.ReservationReleased)
	}
}

func TestReservationRepo_Release_NotFound(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewReservationRepo(db)

	err := repo.Release(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent reservation")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected not_found error, got: %v", err)
	}
}

func TestReservationRepo_Confirm(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM reservations")
		db.Exec("DELETE FROM stock")
	})

	vid := seedVariant(t, db)
	stockRepo := postgres.NewStockRepo(db)
	seedStock(t, stockRepo, vid, 10)

	repo := postgres.NewReservationRepo(db)
	res, _ := inventory.NewReservation(id.New(), vid, 3, time.Now().Add(15*time.Minute))
	if err := repo.Reserve(context.Background(), &res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	if err := repo.Confirm(context.Background(), res.ID); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// Stock should remain decremented (not restored).
	stock, _ := stockRepo.GetStock(context.Background(), vid)
	if stock.Quantity != 7 {
		t.Errorf("stock after confirm: got %d, want 7", stock.Quantity)
	}

	// Reservation should be confirmed.
	found, _ := repo.FindByID(context.Background(), res.ID)
	if found.Status != inventory.ReservationConfirmed {
		t.Errorf("Status = %q, want %q", found.Status, inventory.ReservationConfirmed)
	}
}

func TestReservationRepo_Confirm_NotFound(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewReservationRepo(db)

	err := repo.Confirm(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent reservation")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected not_found error, got: %v", err)
	}
}

func TestReservationRepo_ListActiveByVariantID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM reservations")
		db.Exec("DELETE FROM stock")
	})

	vid := seedVariant(t, db)
	stockRepo := postgres.NewStockRepo(db)
	seedStock(t, stockRepo, vid, 20)

	repo := postgres.NewReservationRepo(db)

	r1, _ := inventory.NewReservation(id.New(), vid, 2, time.Now().Add(15*time.Minute))
	r2, _ := inventory.NewReservation(id.New(), vid, 3, time.Now().Add(15*time.Minute))
	if err := repo.Reserve(context.Background(), &r1); err != nil {
		t.Fatalf("Reserve r1: %v", err)
	}
	if err := repo.Reserve(context.Background(), &r2); err != nil {
		t.Fatalf("Reserve r2: %v", err)
	}

	// Release r1 so only r2 is active.
	if err := repo.Release(context.Background(), r1.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}

	active, err := repo.ListActiveByVariantID(context.Background(), vid)
	if err != nil {
		t.Fatalf("ListActiveByVariantID: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("got %d active reservations, want 1", len(active))
	}
	if active[0].ID != r2.ID {
		t.Errorf("active[0].ID = %q, want %q", active[0].ID, r2.ID)
	}
}

func TestReservationRepo_Reserve_Nil(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewReservationRepo(db)

	err := repo.Reserve(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil reservation")
	}
}

func TestReservationRepo_Release_EmptyID(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewReservationRepo(db)

	err := repo.Release(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestReservationRepo_Confirm_EmptyID(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewReservationRepo(db)

	err := repo.Confirm(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}
