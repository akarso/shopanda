package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestNewSearchCategorySource_NilDB(t *testing.T) {
	if _, err := postgres.NewSearchCategorySource(nil); err == nil {
		t.Fatal("expected error for nil *sql.DB")
	}
}

func TestSearchCategorySource_GetByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	source, err := postgres.NewSearchCategorySource(db)
	if err != nil {
		t.Fatalf("NewSearchCategorySource: %v", err)
	}

	_, found, err := source.GetByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found {
		t.Fatal("found = true for a non-existent category, want false")
	}
}

// TestSearchCategorySource_GetByID_ComputesProductCount is the main
// behavior this port exists for: a fresh, live ProductCount (not a
// snapshot cached anywhere), since that's the whole reason
// IndexUpdateSubscriber re-reads the category rather than trusting the
// event payload (which carries none of this).
func TestSearchCategorySource_GetByID_ComputesProductCount(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM product_categories")
		mustExec(t, db, "DELETE FROM categories")
	})

	parent := mustNewCategory(t, "Parent", "parent-cat-source")
	mustExec(t, db, "INSERT INTO categories (id, parent_id, name, slug, position, meta, created_at, updated_at) VALUES ($1, NULL, $2, $3, 0, '{}'::jsonb, now(), now())", parent.ID, parent.Name, parent.Slug)

	child := mustNewCategory(t, "Child", "child-cat-source")
	mustExec(t, db, "INSERT INTO categories (id, parent_id, name, slug, position, meta, created_at, updated_at) VALUES ($1, $2, $3, $4, 0, '{}'::jsonb, now(), now())", child.ID, parent.ID, child.Name, child.Slug)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	ctx := context.Background()
	p1 := mustNewProduct(t, "P1", "p1-cat-source")
	p2 := mustNewProduct(t, "P2", "p2-cat-source")
	for _, p := range []*catalog.Product{&p1, &p2} {
		if err := productRepo.Create(ctx, p); err != nil {
			t.Fatalf("Create(%s): %v", p.Name, err)
		}
	}
	if err := productRepo.AssignCategory(ctx, p1.ID, child.ID); err != nil {
		t.Fatalf("AssignCategory(p1, child): %v", err)
	}
	if err := productRepo.AssignCategory(ctx, p2.ID, child.ID); err != nil {
		t.Fatalf("AssignCategory(p2, child): %v", err)
	}

	source, err := postgres.NewSearchCategorySource(db)
	if err != nil {
		t.Fatalf("NewSearchCategorySource: %v", err)
	}

	got, found, err := source.GetByID(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if got.Name != "Child" || got.Slug != "child-cat-source" {
		t.Errorf("got = %+v, want Name=Child Slug=child-cat-source", got)
	}
	if got.ParentID != parent.ID {
		t.Errorf("ParentID = %q, want %q", got.ParentID, parent.ID)
	}
	if got.ProductCount != 2 {
		t.Errorf("ProductCount = %d, want 2", got.ProductCount)
	}

	// A root category (no parent) reports ParentID == "".
	gotParent, found, err := source.GetByID(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetByID(parent): %v", err)
	}
	if !found {
		t.Fatal("found = false for parent, want true")
	}
	if gotParent.ParentID != "" {
		t.Errorf("ParentID = %q for a root category, want empty", gotParent.ParentID)
	}
	if gotParent.ProductCount != 0 {
		t.Errorf("ProductCount = %d for parent, want 0 (products are assigned to child, not parent)", gotParent.ProductCount)
	}
}
