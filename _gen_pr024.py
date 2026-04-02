#!/usr/bin/env python3
"""Generate Go source files for PR-024: Inventory reservations."""

import os

BASE = os.path.dirname(os.path.abspath(__file__))


def write(rel_path, content):
    path = os.path.join(BASE, rel_path)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)
    print(f"  wrote {rel_path}")


# ── 1. Domain: reservation types ─────────────────────────────────────────

write("internal/domain/inventory/reservation.go", """\
package inventory

import (
	"errors"
	"time"
)

// ReservationStatus represents the state of a stock reservation.
type ReservationStatus string

const (
	ReservationActive   ReservationStatus = "active"
	ReservationReleased ReservationStatus = "released"
	ReservationConfirmed ReservationStatus = "confirmed"
)

// IsValid returns true if s is a recognised reservation status.
func (s ReservationStatus) IsValid() bool {
	switch s {
	case ReservationActive, ReservationReleased, ReservationConfirmed:
		return true
	}
	return false
}

// Reservation represents a temporary hold on inventory for a variant.
type Reservation struct {
	ID        string
	VariantID string
	Quantity  int
	Status    ReservationStatus
	ExpiresAt time.Time
	CreatedAt time.Time
}

// NewReservation creates a Reservation with validation.
func NewReservation(id, variantID string, quantity int, expiresAt time.Time) (Reservation, error) {
	if id == "" {
		return Reservation{}, errors.New("reservation: id must not be empty")
	}
	if variantID == "" {
		return Reservation{}, errors.New("reservation: variant id must not be empty")
	}
	if quantity <= 0 {
		return Reservation{}, errors.New("reservation: quantity must be positive")
	}
	if expiresAt.IsZero() {
		return Reservation{}, errors.New("reservation: expires_at must not be zero")
	}
	return Reservation{
		ID:        id,
		VariantID: variantID,
		Quantity:  quantity,
		Status:    ReservationActive,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// IsExpired returns true if the reservation has passed its expiry time.
func (r Reservation) IsExpired(now time.Time) bool {
	return now.After(r.ExpiresAt)
}
""")

write("internal/domain/inventory/reservation_test.go", """\
package inventory_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
)

func TestNewReservation_Valid(t *testing.T) {
	exp := time.Now().Add(15 * time.Minute)
	r, err := inventory.NewReservation("res-1", "var-1", 3, exp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.ID != "res-1" {
		t.Errorf("ID = %q, want %q", r.ID, "res-1")
	}
	if r.VariantID != "var-1" {
		t.Errorf("VariantID = %q, want %q", r.VariantID, "var-1")
	}
	if r.Quantity != 3 {
		t.Errorf("Quantity = %d, want 3", r.Quantity)
	}
	if r.Status != inventory.ReservationActive {
		t.Errorf("Status = %q, want %q", r.Status, inventory.ReservationActive)
	}
	if r.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestNewReservation_EmptyID(t *testing.T) {
	_, err := inventory.NewReservation("", "var-1", 1, time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewReservation_EmptyVariantID(t *testing.T) {
	_, err := inventory.NewReservation("res-1", "", 1, time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("expected error for empty variant id")
	}
}

func TestNewReservation_ZeroQuantity(t *testing.T) {
	_, err := inventory.NewReservation("res-1", "var-1", 0, time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

func TestNewReservation_NegativeQuantity(t *testing.T) {
	_, err := inventory.NewReservation("res-1", "var-1", -1, time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("expected error for negative quantity")
	}
}

func TestNewReservation_ZeroExpiresAt(t *testing.T) {
	_, err := inventory.NewReservation("res-1", "var-1", 1, time.Time{})
	if err == nil {
		t.Fatal("expected error for zero expires_at")
	}
}

func TestReservation_IsExpired(t *testing.T) {
	exp := time.Now().Add(-time.Minute)
	r, _ := inventory.NewReservation("res-1", "var-1", 1, exp)
	if !r.IsExpired(time.Now()) {
		t.Error("expected reservation to be expired")
	}
}

func TestReservation_NotExpired(t *testing.T) {
	exp := time.Now().Add(15 * time.Minute)
	r, _ := inventory.NewReservation("res-1", "var-1", 1, exp)
	if r.IsExpired(time.Now()) {
		t.Error("expected reservation to not be expired")
	}
}

func TestReservationStatus_IsValid(t *testing.T) {
	tests := []struct {
		s    inventory.ReservationStatus
		want bool
	}{
		{inventory.ReservationActive, true},
		{inventory.ReservationReleased, true},
		{inventory.ReservationConfirmed, true},
		{"unknown", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.s.IsValid(); got != tt.want {
			t.Errorf("ReservationStatus(%q).IsValid() = %v, want %v", tt.s, got, tt.want)
		}
	}
}
""")

# ── 2. Repository interface ─────────────────────────────────────────────

write("internal/domain/inventory/reservation_repository.go", """\
package inventory

import "context"

// ReservationRepository defines persistence operations for inventory reservations.
type ReservationRepository interface {
	// Reserve atomically decrements stock and creates a reservation.
	// Returns an error if insufficient stock is available.
	Reserve(ctx context.Context, reservation *Reservation) error

	// Release cancels an active reservation and restores the reserved quantity to stock.
	// Returns an error if the reservation is not found or not active.
	Release(ctx context.Context, reservationID string) error

	// Confirm marks a reservation as confirmed without restoring stock
	// (stock was already decremented at reserve time).
	// Returns an error if the reservation is not found or not active.
	Confirm(ctx context.Context, reservationID string) error

	// FindByID returns a reservation by its ID.
	// Returns (nil, nil) when no reservation exists.
	FindByID(ctx context.Context, id string) (*Reservation, error)

	// ListActiveByVariantID returns all active reservations for a variant.
	ListActiveByVariantID(ctx context.Context, variantID string) ([]Reservation, error)
}
""")

# ── 3. Migration ────────────────────────────────────────────────────────

write("migrations/005_create_reservations.sql", """\
CREATE TABLE reservations (
    id          UUID PRIMARY KEY,
    variant_id  UUID NOT NULL REFERENCES variants(id) ON DELETE CASCADE,
    quantity    INT NOT NULL CHECK (quantity > 0),
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'released', 'confirmed')),
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_reservations_variant_status ON reservations (variant_id, status);
CREATE INDEX idx_reservations_expires_at ON reservations (expires_at) WHERE status = 'active';
""")

# ── 4. Postgres implementation ──────────────────────────────────────────

write("internal/infrastructure/postgres/reservation_repo.go", """\
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// Compile-time check that ReservationRepo implements inventory.ReservationRepository.
var _ inventory.ReservationRepository = (*ReservationRepo)(nil)

// ReservationRepo implements inventory.ReservationRepository using PostgreSQL.
type ReservationRepo struct {
	db *sql.DB
}

// NewReservationRepo returns a new ReservationRepo backed by db.
func NewReservationRepo(db *sql.DB) *ReservationRepo {
	return &ReservationRepo{db: db}
}

// Reserve atomically decrements stock and creates a reservation within a transaction.
func (r *ReservationRepo) Reserve(ctx context.Context, res *inventory.Reservation) error {
	if res == nil {
		return fmt.Errorf("reservation_repo: reserve: reservation must not be nil")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reservation_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Decrement stock atomically, fail if insufficient.
	const decr = `UPDATE stock SET quantity = quantity - $1, updated_at = $2
		WHERE variant_id = $3 AND quantity >= $1`
	result, err := tx.ExecContext(ctx, decr, res.Quantity, time.Now().UTC(), res.VariantID)
	if err != nil {
		return fmt.Errorf("reservation_repo: decrement stock: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reservation_repo: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.New(apperror.CodeConflict, "insufficient stock")
	}

	// Insert reservation.
	const ins = `INSERT INTO reservations (id, variant_id, quantity, status, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err = tx.ExecContext(ctx, ins,
		res.ID, res.VariantID, res.Quantity, string(res.Status), res.ExpiresAt, res.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("reservation_repo: insert reservation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reservation_repo: commit: %w", err)
	}
	return nil
}

// Release cancels an active reservation and restores stock.
func (r *ReservationRepo) Release(ctx context.Context, reservationID string) error {
	if reservationID == "" {
		return fmt.Errorf("reservation_repo: release: empty reservation id")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reservation_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Mark reservation as released, returning its variant_id and quantity.
	const upd = `UPDATE reservations SET status = 'released'
		WHERE id = $1 AND status = 'active'
		RETURNING variant_id, quantity`
	var variantID string
	var qty int
	err = tx.QueryRowContext(ctx, upd, reservationID).Scan(&variantID, &qty)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.NotFound("reservation not found or not active")
	}
	if err != nil {
		return fmt.Errorf("reservation_repo: update reservation: %w", err)
	}

	// Restore stock.
	const incr = `UPDATE stock SET quantity = quantity + $1, updated_at = $2
		WHERE variant_id = $3`
	_, err = tx.ExecContext(ctx, incr, qty, time.Now().UTC(), variantID)
	if err != nil {
		return fmt.Errorf("reservation_repo: restore stock: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reservation_repo: commit: %w", err)
	}
	return nil
}

// Confirm marks a reservation as confirmed without restoring stock.
func (r *ReservationRepo) Confirm(ctx context.Context, reservationID string) error {
	if reservationID == "" {
		return fmt.Errorf("reservation_repo: confirm: empty reservation id")
	}

	const q = `UPDATE reservations SET status = 'confirmed'
		WHERE id = $1 AND status = 'active'`
	result, err := r.db.ExecContext(ctx, q, reservationID)
	if err != nil {
		return fmt.Errorf("reservation_repo: confirm: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reservation_repo: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("reservation not found or not active")
	}
	return nil
}

// FindByID returns a reservation by its ID.
func (r *ReservationRepo) FindByID(ctx context.Context, id string) (*inventory.Reservation, error) {
	if id == "" {
		return nil, fmt.Errorf("reservation_repo: find: empty id")
	}
	const q = `SELECT id, variant_id, quantity, status, expires_at, created_at
		FROM reservations WHERE id = $1`
	var res inventory.Reservation
	var status string
	err := r.db.QueryRowContext(ctx, q, id).Scan(
		&res.ID, &res.VariantID, &res.Quantity, &status, &res.ExpiresAt, &res.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reservation_repo: find: %w", err)
	}
	res.Status = inventory.ReservationStatus(status)
	return &res, nil
}

// ListActiveByVariantID returns all active reservations for a variant.
func (r *ReservationRepo) ListActiveByVariantID(ctx context.Context, variantID string) ([]inventory.Reservation, error) {
	if variantID == "" {
		return nil, fmt.Errorf("reservation_repo: list active: empty variant id")
	}
	const q = `SELECT id, variant_id, quantity, status, expires_at, created_at
		FROM reservations WHERE variant_id = $1 AND status = 'active'
		ORDER BY created_at`

	rows, err := r.db.QueryContext(ctx, q, variantID)
	if err != nil {
		return nil, fmt.Errorf("reservation_repo: list active: %w", err)
	}
	defer rows.Close()

	var reservations []inventory.Reservation
	for rows.Next() {
		var res inventory.Reservation
		var status string
		if err := rows.Scan(&res.ID, &res.VariantID, &res.Quantity, &status, &res.ExpiresAt, &res.CreatedAt); err != nil {
			return nil, fmt.Errorf("reservation_repo: list scan: %w", err)
		}
		res.Status = inventory.ReservationStatus(status)
		reservations = append(reservations, res)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reservation_repo: list rows: %w", err)
	}
	return reservations, nil
}
""")

# ── 5. Integration tests ────────────────────────────────────────────────

write("internal/infrastructure/postgres/reservation_repo_test.go", """\
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
""")

print("PR-024 files generated successfully.")
