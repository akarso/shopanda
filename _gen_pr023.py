#!/usr/bin/env python3
"""Generate Go source files for PR-023: Inventory domain + storage."""

import os

BASE = os.path.dirname(os.path.abspath(__file__))


def write(rel_path, content):
    path = os.path.join(BASE, rel_path)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)
    print(f"  wrote {rel_path}")


# ── 1. Domain: inventory package ─────────────────────────────────────────

write("internal/domain/inventory/doc.go", """\
// Package inventory defines stock management types and repository interfaces.
package inventory
""")

write("internal/domain/inventory/stock.go", """\
package inventory

import (
	"errors"
	"time"
)

// StockEntry represents the current stock level for a variant.
type StockEntry struct {
	VariantID string
	Quantity  int
	UpdatedAt time.Time
}

// NewStockEntry creates a StockEntry with validation.
func NewStockEntry(variantID string, quantity int) (StockEntry, error) {
	if variantID == "" {
		return StockEntry{}, errors.New("stock: variant id must not be empty")
	}
	if quantity < 0 {
		return StockEntry{}, errors.New("stock: quantity must not be negative")
	}
	return StockEntry{
		VariantID: variantID,
		Quantity:  quantity,
		UpdatedAt: time.Now().UTC(),
	}, nil
}

// IsAvailable returns true if the stock quantity is greater than zero.
func (s StockEntry) IsAvailable() bool {
	return s.Quantity > 0
}

// HasStock returns true if the stock has at least the requested quantity.
func (s StockEntry) HasStock(needed int) bool {
	return s.Quantity >= needed
}
""")

write("internal/domain/inventory/stock_test.go", """\
package inventory_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/inventory"
)

func TestNewStockEntry_Valid(t *testing.T) {
	s, err := inventory.NewStockEntry("variant-1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.VariantID != "variant-1" {
		t.Errorf("VariantID = %q, want %q", s.VariantID, "variant-1")
	}
	if s.Quantity != 10 {
		t.Errorf("Quantity = %d, want %d", s.Quantity, 10)
	}
	if s.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestNewStockEntry_ZeroQuantity(t *testing.T) {
	s, err := inventory.NewStockEntry("variant-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Quantity != 0 {
		t.Errorf("Quantity = %d, want 0", s.Quantity)
	}
}

func TestNewStockEntry_EmptyVariantID(t *testing.T) {
	_, err := inventory.NewStockEntry("", 10)
	if err == nil {
		t.Fatal("expected error for empty variant id")
	}
}

func TestNewStockEntry_NegativeQuantity(t *testing.T) {
	_, err := inventory.NewStockEntry("variant-1", -1)
	if err == nil {
		t.Fatal("expected error for negative quantity")
	}
}

func TestStockEntry_IsAvailable(t *testing.T) {
	tests := []struct {
		qty  int
		want bool
	}{
		{0, false},
		{1, true},
		{100, true},
	}
	for _, tt := range tests {
		s, _ := inventory.NewStockEntry("v", tt.qty)
		if got := s.IsAvailable(); got != tt.want {
			t.Errorf("IsAvailable() with qty=%d: got %v, want %v", tt.qty, got, tt.want)
		}
	}
}

func TestStockEntry_HasStock(t *testing.T) {
	s, _ := inventory.NewStockEntry("v", 5)
	tests := []struct {
		needed int
		want   bool
	}{
		{0, true},
		{5, true},
		{6, false},
	}
	for _, tt := range tests {
		if got := s.HasStock(tt.needed); got != tt.want {
			t.Errorf("HasStock(%d) with qty=5: got %v, want %v", tt.needed, got, tt.want)
		}
	}
}
""")

# ── 2. Repository interface ─────────────────────────────────────────────

write("internal/domain/inventory/repository.go", """\
package inventory

import "context"

// StockRepository defines persistence operations for inventory stock.
type StockRepository interface {
	// GetStock returns the stock entry for a variant.
	// Returns a zero-quantity entry (not an error) when no stock record exists.
	GetStock(ctx context.Context, variantID string) (StockEntry, error)

	// SetStock sets the absolute stock quantity for a variant.
	// Creates the record if it does not exist, updates it otherwise.
	SetStock(ctx context.Context, entry *StockEntry) error
}
""")

# ── 3. Migration ────────────────────────────────────────────────────────

write("migrations/004_create_stock.sql", """\
CREATE TABLE stock (
    variant_id  UUID PRIMARY KEY REFERENCES variants(id) ON DELETE CASCADE,
    quantity    INT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
""")

# ── 4. Postgres implementation ──────────────────────────────────────────

write("internal/infrastructure/postgres/stock_repo.go", """\
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
)

// Compile-time check that StockRepo implements inventory.StockRepository.
var _ inventory.StockRepository = (*StockRepo)(nil)

// StockRepo implements inventory.StockRepository using PostgreSQL.
type StockRepo struct {
	db *sql.DB
}

// NewStockRepo returns a new StockRepo backed by db.
func NewStockRepo(db *sql.DB) *StockRepo {
	return &StockRepo{db: db}
}

// GetStock returns the stock entry for a variant.
// Returns a zero-quantity entry when no record exists.
func (r *StockRepo) GetStock(ctx context.Context, variantID string) (inventory.StockEntry, error) {
	const q = `SELECT variant_id, quantity, updated_at FROM stock WHERE variant_id = $1`

	var s inventory.StockEntry
	err := r.db.QueryRowContext(ctx, q, variantID).Scan(
		&s.VariantID, &s.Quantity, &s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return inventory.StockEntry{
			VariantID: variantID,
			Quantity:  0,
			UpdatedAt: time.Time{},
		}, nil
	}
	if err != nil {
		return inventory.StockEntry{}, fmt.Errorf("stock_repo: get stock: %w", err)
	}
	return s, nil
}

// SetStock upserts the stock quantity for a variant.
func (r *StockRepo) SetStock(ctx context.Context, entry *inventory.StockEntry) error {
	if entry == nil {
		return fmt.Errorf("stock_repo: set stock: entry must not be nil")
	}
	const q = `INSERT INTO stock (variant_id, quantity, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (variant_id) DO UPDATE
		SET quantity = EXCLUDED.quantity,
		    updated_at = EXCLUDED.updated_at`

	_, err := r.db.ExecContext(ctx, q,
		entry.VariantID, entry.Quantity, entry.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("stock_repo: set stock: %w", err)
	}
	return nil
}
""")

write("internal/infrastructure/postgres/stock_repo_test.go", """\
package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

// seedVariant creates a product + variant in the DB and returns the variant ID.
func seedVariant(t *testing.T, db *sql.DB) string {
	t.Helper()
	ctx := context.Background()
	prodRepo := postgres.NewProductRepo(db)
	prod := mustNewProduct(t, "Stock Product", "stock-"+id.New()[:8])
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	variantRepo := postgres.NewVariantRepo(db)
	v := mustNewVariant(t, prod.ID, "SKU-STOCK-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("seed variant: %v", err)
	}
	return v.ID
}

func TestStockRepo_GetStock_NoRecord(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM stock") })

	repo := postgres.NewStockRepo(db)
	vid := seedVariant(t, db)

	s, err := repo.GetStock(context.Background(), vid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Quantity != 0 {
		t.Errorf("Quantity = %d, want 0", s.Quantity)
	}
	if s.VariantID != vid {
		t.Errorf("VariantID = %q, want %q", s.VariantID, vid)
	}
}

func TestStockRepo_SetStock_Create(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM stock") })

	repo := postgres.NewStockRepo(db)
	vid := seedVariant(t, db)

	entry, err := inventory.NewStockEntry(vid, 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := repo.SetStock(context.Background(), &entry); err != nil {
		t.Fatalf("SetStock: %v", err)
	}

	got, err := repo.GetStock(context.Background(), vid)
	if err != nil {
		t.Fatalf("GetStock: %v", err)
	}
	if got.Quantity != 25 {
		t.Errorf("Quantity = %d, want 25", got.Quantity)
	}
}

func TestStockRepo_SetStock_Update(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM stock") })

	repo := postgres.NewStockRepo(db)
	vid := seedVariant(t, db)

	entry1, _ := inventory.NewStockEntry(vid, 10)
	if err := repo.SetStock(context.Background(), &entry1); err != nil {
		t.Fatalf("SetStock(10): %v", err)
	}

	entry2, _ := inventory.NewStockEntry(vid, 5)
	if err := repo.SetStock(context.Background(), &entry2); err != nil {
		t.Fatalf("SetStock(5): %v", err)
	}

	got, err := repo.GetStock(context.Background(), vid)
	if err != nil {
		t.Fatalf("GetStock: %v", err)
	}
	if got.Quantity != 5 {
		t.Errorf("Quantity = %d, want 5", got.Quantity)
	}
}

func TestStockRepo_SetStock_Nil(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewStockRepo(db)

	err := repo.SetStock(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil entry")
	}
}
""")

print("PR-023 files generated successfully.")
