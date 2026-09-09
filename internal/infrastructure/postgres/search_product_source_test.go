package postgres_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestSearchProductSource_CountAllAndListAll(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	ctx := context.Background()
	for _, name := range []string{"Widget", "Gadget", "Gizmo"} {
		p := mustNewProduct(t, name, name)
		if err := productRepo.Create(ctx, &p); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}

	source, err := postgres.NewSearchProductSource(db)
	if err != nil {
		t.Fatalf("NewSearchProductSource: %v", err)
	}

	count, err := source.CountAll(ctx)
	if err != nil {
		t.Fatalf("CountAll: %v", err)
	}
	if count != 3 {
		t.Fatalf("CountAll = %d, want 3", count)
	}

	var seen []string
	const pageSize = 2
	for offset := 0; ; offset += pageSize {
		page, err := source.ListAll(ctx, offset, pageSize)
		if err != nil {
			t.Fatalf("ListAll(offset=%d): %v", offset, err)
		}
		if len(page) == 0 {
			break
		}
		for _, p := range page {
			seen = append(seen, p.ID)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("paged through %d products, want 3", len(seen))
	}
}

func TestSearchProductSource_ListAll_RejectsInvalidArgs(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	source, _ := postgres.NewSearchProductSource(db)
	ctx := context.Background()

	if _, err := source.ListAll(ctx, -1, 10); err == nil {
		t.Error("expected error for negative offset")
	}
	if _, err := source.ListAll(ctx, 0, 0); err == nil {
		t.Error("expected error for non-positive limit")
	}
}

func TestNewSearchProductSource_NilDB(t *testing.T) {
	if _, err := postgres.NewSearchProductSource(nil); err == nil {
		t.Fatal("expected error for nil *sql.DB")
	}
}

// TestSearchProductSource_ListByIDs covers PR-1034's scoped-scan read path:
// exactly the requested IDs come back, a deleted/nonexistent ID is simply
// absent (not an error — see the port's own doc comment), and an empty ID
// list returns an empty result without querying at all.
func TestSearchProductSource_ListByIDs(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	ctx := context.Background()

	var ids []string
	for _, name := range []string{"Widget", "Gadget", "Gizmo"} {
		p := mustNewProduct(t, name, name)
		if err := productRepo.Create(ctx, &p); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		ids = append(ids, p.ID)
	}

	source, _ := postgres.NewSearchProductSource(db)

	// deletedID is a well-formed but nonexistent UUID — the realistic
	// shape of "deleted between ReindexService.Trigger resolving the
	// scope and the job actually running" (see ListByIDs' own doc
	// comment), not an arbitrary malformed string: every product ID in
	// this codebase comes from id.New(), and the products.id column is
	// itself UUID-typed, so a non-UUID string is not a case this port
	// needs to tolerate gracefully.
	deletedID := id.New()
	got, err := source.ListByIDs(ctx, []string{ids[0], ids[2], deletedID})
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	var gotIDs []string
	for _, p := range got {
		gotIDs = append(gotIDs, p.ID)
	}
	sort.Strings(gotIDs)
	want := []string{ids[0], ids[2]}
	sort.Strings(want)
	if len(gotIDs) != len(want) || gotIDs[0] != want[0] || gotIDs[1] != want[1] {
		t.Fatalf("ListByIDs returned %v, want %v (missing id silently dropped, not an error)", gotIDs, want)
	}

	empty, err := source.ListByIDs(ctx, nil)
	if err != nil {
		t.Fatalf("ListByIDs(nil): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("ListByIDs(nil) = %v, want empty", empty)
	}
}

// TestSearchProductSource_ProductIDsByCategory covers PR-1034's
// ScopeCategories resolution: the distinct product IDs assigned to any of
// the given categories, deduplicated when a product belongs to more than
// one requested category.
func TestSearchProductSource_ProductIDsByCategory(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM product_categories")
		mustExec(t, db, "DELETE FROM categories")
	})

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	ctx := context.Background()

	inCat1 := mustNewProduct(t, "In Cat1", "in-cat1")
	inBoth := mustNewProduct(t, "In Both", "in-both")
	inNeither := mustNewProduct(t, "In Neither", "in-neither")
	for _, p := range []*catalog.Product{&inCat1, &inBoth, &inNeither} {
		if err := productRepo.Create(ctx, p); err != nil {
			t.Fatalf("Create(%s): %v", p.Name, err)
		}
	}

	cat1 := mustNewCategory(t, "Cat1", "cat1-by-category")
	cat2 := mustNewCategory(t, "Cat2", "cat2-by-category")
	for _, c := range []catalog.Category{cat1, cat2} {
		mustExec(t, db, "INSERT INTO categories (id, parent_id, name, slug, position, meta, created_at, updated_at) VALUES ($1, NULL, $2, $3, 0, '{}'::jsonb, now(), now())", c.ID, c.Name, c.Slug)
	}

	if err := productRepo.AssignCategory(ctx, inCat1.ID, cat1.ID); err != nil {
		t.Fatalf("AssignCategory(inCat1, cat1): %v", err)
	}
	if err := productRepo.AssignCategory(ctx, inBoth.ID, cat1.ID); err != nil {
		t.Fatalf("AssignCategory(inBoth, cat1): %v", err)
	}
	if err := productRepo.AssignCategory(ctx, inBoth.ID, cat2.ID); err != nil {
		t.Fatalf("AssignCategory(inBoth, cat2): %v", err)
	}

	source, _ := postgres.NewSearchProductSource(db)

	got, err := source.ProductIDsByCategory(ctx, []string{cat1.ID, cat2.ID})
	if err != nil {
		t.Fatalf("ProductIDsByCategory: %v", err)
	}
	sort.Strings(got)
	want := []string{inBoth.ID, inCat1.ID}
	sort.Strings(want)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ProductIDsByCategory = %v, want %v (distinct, excludes the unassigned product)", got, want)
	}
}

func TestSearchProductSource_ProductIDsByCategory_RequiresAtLeastOneID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	source, _ := postgres.NewSearchProductSource(db)

	if _, err := source.ProductIDsByCategory(context.Background(), nil); err == nil {
		t.Fatal("expected an error for an empty category ID list")
	}
}

// TestSearchProductSource_ProductIDsUpdatedSince covers PR-1034's
// ScopeSince resolution: only products whose updated_at is at or after
// the cutoff are returned.
func TestSearchProductSource_ProductIDsUpdatedSince(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	ctx := context.Background()

	older := mustNewProduct(t, "Older", "older-updated-since")
	newer := mustNewProduct(t, "Newer", "newer-updated-since")
	for _, p := range []*catalog.Product{&older, &newer} {
		if err := productRepo.Create(ctx, p); err != nil {
			t.Fatalf("Create(%s): %v", p.Name, err)
		}
	}

	cutoff := time.Now().UTC()
	mustExec(t, db, "UPDATE products SET updated_at = $1 WHERE id = $2", cutoff.Add(-time.Hour), older.ID)
	mustExec(t, db, "UPDATE products SET updated_at = $1 WHERE id = $2", cutoff.Add(time.Hour), newer.ID)

	source, _ := postgres.NewSearchProductSource(db)

	got, err := source.ProductIDsUpdatedSince(ctx, cutoff)
	if err != nil {
		t.Fatalf("ProductIDsUpdatedSince: %v", err)
	}
	if len(got) != 1 || got[0] != newer.ID {
		t.Fatalf("ProductIDsUpdatedSince(cutoff) = %v, want exactly [%s]", got, newer.ID)
	}
}
