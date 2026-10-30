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

// TestSearchProductSource_ListAll_PopulatesCategoryIDs is the regression
// test for PR-1037: search.Product.CategoryID (singular) silently dropped
// every category past the first for a multi-category product. ListAll
// must return every assigned category, not just one, and a product with
// no assignments must get an empty (not nil-vs-empty-ambiguous) slice.
func TestSearchProductSource_ListAll_PopulatesCategoryIDs(t *testing.T) {
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

	multi := mustNewProduct(t, "Multi Category", "multi-category-ids")
	none := mustNewProduct(t, "No Category", "no-category-ids")
	for _, p := range []*catalog.Product{&multi, &none} {
		if err := productRepo.Create(ctx, p); err != nil {
			t.Fatalf("Create(%s): %v", p.Name, err)
		}
	}

	cat1 := mustNewCategory(t, "Cat1", "cat1-product-source")
	cat2 := mustNewCategory(t, "Cat2", "cat2-product-source")
	for _, c := range []catalog.Category{cat1, cat2} {
		mustExec(t, db, "INSERT INTO categories (id, parent_id, name, slug, position, meta, created_at, updated_at) VALUES ($1, NULL, $2, $3, 0, '{}'::jsonb, now(), now())", c.ID, c.Name, c.Slug)
	}
	if err := productRepo.AssignCategory(ctx, multi.ID, cat1.ID); err != nil {
		t.Fatalf("AssignCategory(multi, cat1): %v", err)
	}
	if err := productRepo.AssignCategory(ctx, multi.ID, cat2.ID); err != nil {
		t.Fatalf("AssignCategory(multi, cat2): %v", err)
	}

	source, _ := postgres.NewSearchProductSource(db)

	all, err := source.ListAll(ctx, 0, 100)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	byID := make(map[string][]string, len(all))
	for _, p := range all {
		byID[p.ID] = p.CategoryIDs
	}
	gotMulti := append([]string(nil), byID[multi.ID]...)
	sort.Strings(gotMulti)
	wantMulti := []string{cat1.ID, cat2.ID}
	sort.Strings(wantMulti)
	if len(gotMulti) != 2 || gotMulti[0] != wantMulti[0] || gotMulti[1] != wantMulti[1] {
		t.Fatalf("ListAll CategoryIDs for multi-category product = %v, want %v", gotMulti, wantMulti)
	}
	if len(byID[none.ID]) != 0 {
		t.Fatalf("ListAll CategoryIDs for unassigned product = %v, want empty", byID[none.ID])
	}

	// ListByIDs must populate the same way.
	byIDs, err := source.ListByIDs(ctx, []string{multi.ID})
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(byIDs) != 1 {
		t.Fatalf("ListByIDs returned %d products, want 1", len(byIDs))
	}
	got := append([]string(nil), byIDs[0].CategoryIDs...)
	sort.Strings(got)
	if len(got) != 2 || got[0] != wantMulti[0] || got[1] != wantMulti[1] {
		t.Fatalf("ListByIDs CategoryIDs = %v, want %v", got, wantMulti)
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

// TestSearchProductSource_ListAll_TypeAndVisibilityNilFacts pins PR-1079:
// Type is selected from products.type, and with no factsAdmin wired active
// products get all four visibility bools true (backwards-compat empty-index
// guard). Non-active rows still index as all-false (storefront-effective).
func TestSearchProductSource_ListAll_TypeAndVisibilityNilFacts(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	p := mustNewProduct(t, "Typed Widget", "typed-widget-vis")
	p.Type = catalog.TypeVirtual
	if err := productRepo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	p.Status = catalog.StatusActive
	if err := productRepo.Update(ctx, &p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	source, err := postgres.NewSearchProductSource(db)
	if err != nil {
		t.Fatalf("NewSearchProductSource: %v", err)
	}
	page, err := source.ListAll(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	var found *struct{}
	for i := range page {
		if page[i].ID != p.ID {
			continue
		}
		found = &struct{}{}
		if page[i].Type != string(catalog.TypeVirtual) {
			t.Fatalf("Type = %q, want %q", page[i].Type, catalog.TypeVirtual)
		}
		if page[i].Status != string(catalog.StatusActive) {
			t.Fatalf("Status = %q, want active", page[i].Status)
		}
		if !page[i].VisibleInCatalog || !page[i].VisibleInSearch || !page[i].VisibleIndividually || !page[i].Purchasable {
			t.Fatalf("nil factsAdmin + active should set all visibility true, got catalog=%v search=%v individually=%v purchasable=%v",
				page[i].VisibleInCatalog, page[i].VisibleInSearch, page[i].VisibleIndividually, page[i].Purchasable)
		}
	}
	if found == nil {
		t.Fatal("product not returned by ListAll")
	}
}

type stubVisibilityFactsAdmin map[string]catalog.VisibilitySnapshot

func (s stubVisibilityFactsAdmin) LoadManyAnyStatus(_ context.Context, ids []string) (map[string]catalog.VisibilitySnapshot, error) {
	out := make(map[string]catalog.VisibilitySnapshot, len(ids))
	for _, id := range ids {
		if snap, ok := s[id]; ok {
			out[id] = snap
		}
	}
	return out, nil
}

// TestSearchProductSource_WithVisibility_MissingSnapshotFailClosed indexes
// missing facts as all-false (PR-1079 review).
func TestSearchProductSource_WithVisibility_MissingSnapshotFailClosed(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	p := mustNewProduct(t, "No Snap", "no-snap-vis")
	if err := productRepo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	p.Status = catalog.StatusActive
	if err := productRepo.Update(ctx, &p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	source, err := postgres.NewSearchProductSource(db)
	if err != nil {
		t.Fatalf("NewSearchProductSource: %v", err)
	}
	source.WithVisibility(stubVisibilityFactsAdmin{}, catalog.VisibilityOptions{})

	page, err := source.ListByIDs(ctx, []string{p.ID})
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("len = %d, want 1", len(page))
	}
	if page[0].VisibleInCatalog || page[0].VisibleInSearch || page[0].VisibleIndividually || page[0].Purchasable {
		t.Fatalf("missing snapshot must fail closed, got catalog=%v search=%v individually=%v purchasable=%v",
			page[0].VisibleInCatalog, page[0].VisibleInSearch, page[0].VisibleIndividually, page[0].Purchasable)
	}
}

// TestSearchProductSource_WithVisibility_ForcedVisibleDraftHidden ensures
// storefront-effective axes are false when Status != active even if modes
// force visible (PR-1079 review).
func TestSearchProductSource_WithVisibility_ForcedVisibleDraftHidden(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	p := mustNewProduct(t, "Draft Forced", "draft-forced-vis")
	p.VisibilityModes.Catalog = catalog.VisibilityModeVisible
	p.VisibilityModes.Search = catalog.VisibilityModeVisible
	p.VisibilityModes.Individually = catalog.VisibilityModeVisible
	p.VisibilityModes.Purchasable = catalog.VisibilityModeVisible
	if err := productRepo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Leave Status as draft (NewProduct default).

	factsRepo, err := postgres.NewVisibilityFactsRepo(db)
	if err != nil {
		t.Fatalf("NewVisibilityFactsRepo: %v", err)
	}
	source, err := postgres.NewSearchProductSource(db)
	if err != nil {
		t.Fatalf("NewSearchProductSource: %v", err)
	}
	source.WithVisibility(factsRepo, catalog.VisibilityOptions{})

	page, err := source.ListByIDs(ctx, []string{p.ID})
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("len = %d, want 1", len(page))
	}
	if page[0].Status != string(catalog.StatusDraft) {
		t.Fatalf("Status = %q, want draft", page[0].Status)
	}
	if page[0].VisibleInCatalog || page[0].VisibleInSearch || page[0].VisibleIndividually || page[0].Purchasable {
		t.Fatalf("forced-visible draft must index as hidden, got catalog=%v search=%v individually=%v purchasable=%v",
			page[0].VisibleInCatalog, page[0].VisibleInSearch, page[0].VisibleIndividually, page[0].Purchasable)
	}
}

// TestSearchProductSource_WithVisibility_HiddenSearch resolves axes via
// LoadManyAnyStatus + Resolve (PR-1079).
func TestSearchProductSource_WithVisibility_HiddenSearch(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	p := mustNewProduct(t, "Hidden Search", "hidden-search-vis")
	p.VisibilityModes.Search = catalog.VisibilityModeHidden
	if err := productRepo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	p.Status = catalog.StatusActive
	if err := productRepo.Update(ctx, &p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	v := mustNewVariant(t, p.ID, "SKU-HS-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("variant Create: %v", err)
	}

	factsRepo, err := postgres.NewVisibilityFactsRepo(db)
	if err != nil {
		t.Fatalf("NewVisibilityFactsRepo: %v", err)
	}
	source, err := postgres.NewSearchProductSource(db)
	if err != nil {
		t.Fatalf("NewSearchProductSource: %v", err)
	}
	source.WithVisibility(factsRepo, catalog.VisibilityOptions{})

	page, err := source.ListByIDs(ctx, []string{p.ID})
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("len = %d, want 1", len(page))
	}
	if page[0].VisibleInSearch {
		t.Fatal("VisibleInSearch should be false for forced-hidden search axis")
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
