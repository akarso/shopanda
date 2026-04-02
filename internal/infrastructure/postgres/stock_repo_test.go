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
