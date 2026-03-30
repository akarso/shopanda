package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"

	_ "github.com/lib/pq"
)

// testDB opens a connection to the test database.
// Set SHOPANDA_TEST_DSN to point at a test Postgres instance.
// Tests are skipped when the env var is not set.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set; skipping integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ensureProductsTable creates the products table for testing.
func ensureProductsTable(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS products (
			id          UUID PRIMARY KEY,
			name        TEXT NOT NULL,
			slug        TEXT UNIQUE NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			status      TEXT NOT NULL DEFAULT 'draft',
			attributes  JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		t.Fatalf("ensure products table: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM products")
	})
}

func mustNewProduct(t *testing.T, name, slug string) catalog.Product {
	t.Helper()
	p, err := catalog.NewProduct(id.New(), name, slug)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	return p
}

func TestProductRepo_CreateAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo := postgres.NewProductRepo(db)
	ctx := context.Background()

	p := mustNewProduct(t, "Widget", "widget")
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
		t.Errorf("ID = %q, want %q", got.ID, p.ID)
	}
	if got.Name != "Widget" {
		t.Errorf("Name = %q, want Widget", got.Name)
	}
	if got.Slug != "widget" {
		t.Errorf("Slug = %q, want widget", got.Slug)
	}
	if got.Status != catalog.StatusDraft {
		t.Errorf("Status = %q, want draft", got.Status)
	}
}

func TestProductRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo := postgres.NewProductRepo(db)
	ctx := context.Background()

	got, err := repo.FindByID(ctx, id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil product for non-existent ID")
	}
}

func TestProductRepo_FindBySlug(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo := postgres.NewProductRepo(db)
	ctx := context.Background()

	p := mustNewProduct(t, "Gizmo", "gizmo")
	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindBySlug(ctx, "gizmo")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	if got == nil {
		t.Fatal("FindBySlug returned nil")
	}
	if got.ID != p.ID {
		t.Errorf("ID = %q, want %q", got.ID, p.ID)
	}
}

func TestProductRepo_FindBySlug_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo := postgres.NewProductRepo(db)
	ctx := context.Background()

	got, err := repo.FindBySlug(ctx, "no-such-slug")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	if got != nil {
		t.Error("expected nil product")
	}
}

func TestProductRepo_List(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo := postgres.NewProductRepo(db)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		p := mustNewProduct(t, fmt.Sprintf("Product %d", i), fmt.Sprintf("product-%d", i))
		p.CreatedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		p.UpdatedAt = p.CreatedAt
		if err := repo.Create(ctx, &p); err != nil {
			t.Fatalf("Create[%d]: %v", i, err)
		}
	}

	// Page 1: limit 2
	products, err := repo.List(ctx, 0, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("len = %d, want 2", len(products))
	}

	// Page 2: offset 2, limit 2
	products, err = repo.List(ctx, 2, 2)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(products) != 1 {
		t.Errorf("len = %d, want 1", len(products))
	}
}

func TestProductRepo_List_ValidationErrors(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo := postgres.NewProductRepo(db)
	ctx := context.Background()

	_, err := repo.List(ctx, -1, 10)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Errorf("negative offset: got %v, want validation error", err)
	}

	_, err = repo.List(ctx, 0, 0)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Errorf("zero limit: got %v, want validation error", err)
	}

	_, err = repo.List(ctx, 0, -5)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Errorf("negative limit: got %v, want validation error", err)
	}
}

func TestProductRepo_Update(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo := postgres.NewProductRepo(db)
	ctx := context.Background()

	p := mustNewProduct(t, "Old Name", "old-name")
	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	p.Name = "New Name"
	p.Status = catalog.StatusActive
	if err := repo.Update(ctx, &p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Name != "New Name" {
		t.Errorf("Name = %q, want New Name", got.Name)
	}
	if got.Status != catalog.StatusActive {
		t.Errorf("Status = %q, want active", got.Status)
	}
}

func TestProductRepo_Update_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo := postgres.NewProductRepo(db)
	ctx := context.Background()

	p := mustNewProduct(t, "Ghost", "ghost")
	err := repo.Update(ctx, &p)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("got %v, want not_found error", err)
	}
}

func TestProductRepo_Create_DuplicateSlug(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo := postgres.NewProductRepo(db)
	ctx := context.Background()

	p1 := mustNewProduct(t, "First", "same-slug")
	if err := repo.Create(ctx, &p1); err != nil {
		t.Fatalf("Create first: %v", err)
	}

	p2 := mustNewProduct(t, "Second", "same-slug")
	err := repo.Create(ctx, &p2)
	if err == nil {
		t.Fatal("expected error for duplicate slug")
	}
}

func TestProductRepo_Attributes(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo := postgres.NewProductRepo(db)
	ctx := context.Background()

	p := mustNewProduct(t, "Fancy", "fancy")
	p.Attributes["color"] = "red"
	p.Attributes["size"] = "M"

	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Attributes["color"] != "red" {
		t.Errorf("color = %v, want red", got.Attributes["color"])
	}
	if got.Attributes["size"] != "M" {
		t.Errorf("size = %v, want M", got.Attributes["size"])
	}
}
