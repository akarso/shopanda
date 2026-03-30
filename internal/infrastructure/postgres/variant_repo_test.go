package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ensureVariantsTable creates the variants table (and products for FK).
func ensureVariantsTable(t *testing.T, db *sql.DB) {
	t.Helper()
	ensureProductsTable(t, db)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS variants (
			id          UUID PRIMARY KEY,
			product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
			sku         TEXT UNIQUE NOT NULL,
			name        TEXT NOT NULL DEFAULT '',
			attributes  JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		t.Fatalf("ensure variants table: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM variants")
	})
}

func mustNewVariant(t *testing.T, productID, sku string) catalog.Variant {
	t.Helper()
	v, err := catalog.NewVariant(id.New(), productID, sku)
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	return v
}

func createTestProduct(t *testing.T, db *sql.DB) catalog.Product {
	t.Helper()
	repo := postgres.NewProductRepo(db)
	p := mustNewProduct(t, "Test Product", "test-product-"+id.New()[:8])
	if err := repo.Create(context.Background(), &p); err != nil {
		t.Fatalf("create test product: %v", err)
	}
	return p
}

func TestVariantRepo_CreateAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	repo := postgres.NewVariantRepo(db)
	ctx := context.Background()

	p := createTestProduct(t, db)
	v := mustNewVariant(t, p.ID, "SKU-001")
	v.Name = "Size M"

	if err := repo.Create(ctx, &v); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, v.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.ID != v.ID {
		t.Errorf("ID = %q, want %q", got.ID, v.ID)
	}
	if got.ProductID != p.ID {
		t.Errorf("ProductID = %q, want %q", got.ProductID, p.ID)
	}
	if got.SKU != "SKU-001" {
		t.Errorf("SKU = %q, want SKU-001", got.SKU)
	}
	if got.Name != "Size M" {
		t.Errorf("Name = %q, want 'Size M'", got.Name)
	}
}

func TestVariantRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	repo := postgres.NewVariantRepo(db)
	ctx := context.Background()

	got, err := repo.FindByID(ctx, id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil variant")
	}
}

func TestVariantRepo_FindBySKU(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	repo := postgres.NewVariantRepo(db)
	ctx := context.Background()

	p := createTestProduct(t, db)
	v := mustNewVariant(t, p.ID, "SKU-FIND")
	if err := repo.Create(ctx, &v); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindBySKU(ctx, "SKU-FIND")
	if err != nil {
		t.Fatalf("FindBySKU: %v", err)
	}
	if got == nil {
		t.Fatal("FindBySKU returned nil")
	}
	if got.ID != v.ID {
		t.Errorf("ID = %q, want %q", got.ID, v.ID)
	}
}

func TestVariantRepo_FindBySKU_NotFound(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	repo := postgres.NewVariantRepo(db)
	ctx := context.Background()

	got, err := repo.FindBySKU(ctx, "NO-SUCH-SKU")
	if err != nil {
		t.Fatalf("FindBySKU: %v", err)
	}
	if got != nil {
		t.Error("expected nil variant")
	}
}

func TestVariantRepo_ListByProductID(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	repo := postgres.NewVariantRepo(db)
	ctx := context.Background()

	p := createTestProduct(t, db)
	for i := 0; i < 3; i++ {
		v := mustNewVariant(t, p.ID, "LIST-SKU-"+id.New()[:8])
		if err := repo.Create(ctx, &v); err != nil {
			t.Fatalf("Create[%d]: %v", i, err)
		}
	}

	variants, err := repo.ListByProductID(ctx, p.ID)
	if err != nil {
		t.Fatalf("ListByProductID: %v", err)
	}
	if len(variants) != 3 {
		t.Errorf("len = %d, want 3", len(variants))
	}
	for _, v := range variants {
		if v.ProductID != p.ID {
			t.Errorf("ProductID = %q, want %q", v.ProductID, p.ID)
		}
	}
}

func TestVariantRepo_ListByProductID_Empty(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	repo := postgres.NewVariantRepo(db)
	ctx := context.Background()

	variants, err := repo.ListByProductID(ctx, id.New())
	if err != nil {
		t.Fatalf("ListByProductID: %v", err)
	}
	if variants != nil {
		t.Errorf("expected nil slice, got len %d", len(variants))
	}
}

func TestVariantRepo_Update(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	repo := postgres.NewVariantRepo(db)
	ctx := context.Background()

	p := createTestProduct(t, db)
	v := mustNewVariant(t, p.ID, "UPD-SKU")
	v.Name = "Original"
	if err := repo.Create(ctx, &v); err != nil {
		t.Fatalf("Create: %v", err)
	}

	v.Name = "Updated"
	v.SKU = "UPD-SKU-NEW"
	if err := repo.Update(ctx, &v); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByID(ctx, v.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Name != "Updated" {
		t.Errorf("Name = %q, want Updated", got.Name)
	}
	if got.SKU != "UPD-SKU-NEW" {
		t.Errorf("SKU = %q, want UPD-SKU-NEW", got.SKU)
	}
	if got.UpdatedAt.Equal(v.CreatedAt) {
		t.Error("UpdatedAt should differ from CreatedAt after update")
	}
}

func TestVariantRepo_Update_NotFound(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	repo := postgres.NewVariantRepo(db)
	ctx := context.Background()

	v := mustNewVariant(t, id.New(), "GHOST-SKU")
	err := repo.Update(ctx, &v)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected not_found, got %v", err)
	}
}

func TestVariantRepo_Create_DuplicateSKU(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	repo := postgres.NewVariantRepo(db)
	ctx := context.Background()

	p := createTestProduct(t, db)
	v1 := mustNewVariant(t, p.ID, "DUP-SKU")
	if err := repo.Create(ctx, &v1); err != nil {
		t.Fatalf("Create first: %v", err)
	}

	v2 := mustNewVariant(t, p.ID, "DUP-SKU")
	err := repo.Create(ctx, &v2)
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Errorf("expected conflict, got %v", err)
	}
}

func TestVariantRepo_Attributes(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	repo := postgres.NewVariantRepo(db)
	ctx := context.Background()

	p := createTestProduct(t, db)
	v := mustNewVariant(t, p.ID, "ATTR-SKU")
	v.Attributes = map[string]interface{}{
		"size":  "M",
		"color": "red",
	}
	if err := repo.Create(ctx, &v); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, v.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Attributes["size"] != "M" {
		t.Errorf("attributes[size] = %v, want M", got.Attributes["size"])
	}
	if got.Attributes["color"] != "red" {
		t.Errorf("attributes[color] = %v, want red", got.Attributes["color"])
	}
}
