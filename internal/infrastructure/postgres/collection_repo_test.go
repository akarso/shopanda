package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewCollection(t *testing.T, name, slug string) catalog.Collection {
	t.Helper()
	c, err := catalog.NewCollection(id.New(), name, slug, catalog.CollectionManual)
	if err != nil {
		t.Fatalf("NewCollection: %v", err)
	}
	return c
}

func TestCollectionRepo_NilDB(t *testing.T) {
	_, err := postgres.NewCollectionRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestCollectionRepo_CreateAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM collection_products")
	db.Exec("DELETE FROM collections")
	t.Cleanup(func() {
		db.Exec("DELETE FROM collection_products")
		db.Exec("DELETE FROM collections")
	})

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCollection(t, "Summer Sale", "summer-sale")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.Name != "Summer Sale" {
		t.Errorf("Name: got %q, want %q", got.Name, "Summer Sale")
	}
	if got.Slug != "summer-sale" {
		t.Errorf("Slug: got %q, want %q", got.Slug, "summer-sale")
	}
	if got.Type != catalog.CollectionManual {
		t.Errorf("Type: got %q, want %q", got.Type, catalog.CollectionManual)
	}
}

func TestCollectionRepo_FindBySlug(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM collection_products")
	db.Exec("DELETE FROM collections")
	t.Cleanup(func() {
		db.Exec("DELETE FROM collection_products")
		db.Exec("DELETE FROM collections")
	})

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCollection(t, "Winter Deal", "winter-deal")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindBySlug(ctx, "winter-deal")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	if got == nil {
		t.Fatal("FindBySlug returned nil")
	}
	if got.ID != c.ID {
		t.Errorf("ID: got %q, want %q", got.ID, c.ID)
	}
}

func TestCollectionRepo_List(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM collection_products")
	db.Exec("DELETE FROM collections")
	t.Cleanup(func() {
		db.Exec("DELETE FROM collection_products")
		db.Exec("DELETE FROM collections")
	})

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}
	ctx := context.Background()

	c1 := mustNewCollection(t, "Alpha", "alpha-coll")
	c2 := mustNewCollection(t, "Beta", "beta-coll")
	for _, c := range []*catalog.Collection{&c1, &c2} {
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("Create %q: %v", c.Name, err)
		}
	}

	all, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List: got %d, want 2", len(all))
	}
	// ordered by name ASC
	if all[0].Name != "Alpha" {
		t.Errorf("first: got %q, want Alpha", all[0].Name)
	}
}

func TestCollectionRepo_Update(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM collection_products")
	db.Exec("DELETE FROM collections")
	t.Cleanup(func() {
		db.Exec("DELETE FROM collection_products")
		db.Exec("DELETE FROM collections")
	})

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCollection(t, "Before", "before-slug")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	c.Name = "After"
	if err := repo.Update(ctx, &c); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Name != "After" {
		t.Errorf("Name: got %q, want %q", got.Name, "After")
	}
}

func TestCollectionRepo_Update_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}

	c := mustNewCollection(t, "Ghost", "ghost-coll")
	err = repo.Update(context.Background(), &c)
	if err == nil {
		t.Fatal("expected error updating non-existent collection")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound, got: %v", err)
	}
}

func TestCollectionRepo_Create_DuplicateSlug(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM collection_products")
	db.Exec("DELETE FROM collections")
	t.Cleanup(func() {
		db.Exec("DELETE FROM collection_products")
		db.Exec("DELETE FROM collections")
	})

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}
	ctx := context.Background()

	c1 := mustNewCollection(t, "First", "dup-slug")
	if err := repo.Create(ctx, &c1); err != nil {
		t.Fatalf("Create: %v", err)
	}

	c2 := mustNewCollection(t, "Second", "dup-slug")
	err = repo.Create(ctx, &c2)
	if err == nil {
		t.Fatal("expected conflict for duplicate slug")
	}
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Errorf("expected Conflict, got: %v", err)
	}
}

func TestCollectionRepo_AddProduct(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM collection_products")
	db.Exec("DELETE FROM collections")
	t.Cleanup(func() {
		db.Exec("DELETE FROM collection_products")
		db.Exec("DELETE FROM collections")
	})

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCollection(t, "Manual Coll", "manual-coll")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create collection: %v", err)
	}

	// Seed a product row for the FK.
	prodID := id.New()
	_, err = db.Exec(`INSERT INTO products (id, name, slug) VALUES ($1, $2, $3)`, prodID, "Test Product", "test-product-"+prodID[:8])
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM products WHERE id = $1", prodID) })

	if err := repo.AddProduct(ctx, c.ID, prodID); err != nil {
		t.Fatalf("AddProduct: %v", err)
	}

	ids, err := repo.ListProductIDs(ctx, c.ID)
	if err != nil {
		t.Fatalf("ListProductIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != prodID {
		t.Errorf("ListProductIDs: got %v, want [%s]", ids, prodID)
	}
}

func TestCollectionRepo_AddProduct_Duplicate(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM collection_products")
	db.Exec("DELETE FROM collections")
	t.Cleanup(func() {
		db.Exec("DELETE FROM collection_products")
		db.Exec("DELETE FROM collections")
	})

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCollection(t, "Dup Add Coll", "dup-add-coll")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	prodID := id.New()
	_, err = db.Exec(`INSERT INTO products (id, name, slug) VALUES ($1, $2, $3)`, prodID, "Dup Prod", "dup-prod-"+prodID[:8])
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM products WHERE id = $1", prodID) })

	if err := repo.AddProduct(ctx, c.ID, prodID); err != nil {
		t.Fatalf("first AddProduct: %v", err)
	}

	err = repo.AddProduct(ctx, c.ID, prodID)
	if err == nil {
		t.Fatal("expected conflict for duplicate add")
	}
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Errorf("expected Conflict, got: %v", err)
	}
}

func TestCollectionRepo_RemoveProduct(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM collection_products")
	db.Exec("DELETE FROM collections")
	t.Cleanup(func() {
		db.Exec("DELETE FROM collection_products")
		db.Exec("DELETE FROM collections")
	})

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCollection(t, "Remove Coll", "remove-coll")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	prodID := id.New()
	_, err = db.Exec(`INSERT INTO products (id, name, slug) VALUES ($1, $2, $3)`, prodID, "Rem Prod", "rem-prod-"+prodID[:8])
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM products WHERE id = $1", prodID) })

	if err := repo.AddProduct(ctx, c.ID, prodID); err != nil {
		t.Fatalf("AddProduct: %v", err)
	}

	if err := repo.RemoveProduct(ctx, c.ID, prodID); err != nil {
		t.Fatalf("RemoveProduct: %v", err)
	}

	ids, err := repo.ListProductIDs(ctx, c.ID)
	if err != nil {
		t.Fatalf("ListProductIDs: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty list after remove, got %v", ids)
	}
}

func TestCollectionRepo_RemoveProduct_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM collection_products")
	db.Exec("DELETE FROM collections")
	t.Cleanup(func() {
		db.Exec("DELETE FROM collection_products")
		db.Exec("DELETE FROM collections")
	})

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCollection(t, "Empty Coll", "empty-coll")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = repo.RemoveProduct(ctx, c.ID, id.New())
	if err == nil {
		t.Fatal("expected error removing non-existent product")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound, got: %v", err)
	}
}

func TestCollectionRepo_FindByID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}

	_, err = repo.FindByID(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestCollectionRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}

	got, err := repo.FindByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent collection")
	}
}

func TestCollectionRepo_Create_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}

	if err := repo.Create(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil collection")
	}
}

func TestCollectionRepo_ListProductIDs_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCollectionRepo(db)
	if err != nil {
		t.Fatalf("NewCollectionRepo: %v", err)
	}

	_, err = repo.ListProductIDs(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty collection id")
	}
}
