package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestVisibilityFactsRepo_LoadManyAnyStatus_IncludesNonActive(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	ctx := context.Background()
	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	prod := mustNewProduct(t, "Draft Widget", "draft-widget-"+id.New()[:8])
	// NewProduct defaults to draft — leave it there; LoadMany (active-only)
	// must miss it while LoadManyAnyStatus must still find it.
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM products WHERE id = $1", prod.ID) })

	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	v := mustNewVariant(t, prod.ID, "SKU-VIS-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("create variant: %v", err)
	}
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM stock WHERE variant_id = $1", v.ID)
		mustExec(t, db, "DELETE FROM prices WHERE variant_id = $1", v.ID)
		mustExec(t, db, "DELETE FROM variants WHERE id = $1", v.ID)
	})

	stockRepo, err := postgres.NewStockRepo(db)
	if err != nil {
		t.Fatalf("NewStockRepo: %v", err)
	}
	entry, err := inventory.NewStockEntry(v.ID, 10)
	if err != nil {
		t.Fatalf("NewStockEntry: %v", err)
	}
	if err := stockRepo.SetStock(ctx, &entry); err != nil {
		t.Fatalf("SetStock: %v", err)
	}

	priceRepo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}
	money := shared.MustNewMoney(999, "EUR")
	price, err := pricing.NewPrice(id.New(), v.ID, "", money)
	if err != nil {
		t.Fatalf("NewPrice: %v", err)
	}
	if err := priceRepo.Upsert(ctx, &price); err != nil {
		t.Fatalf("Upsert price: %v", err)
	}

	factsRepo, err := postgres.NewVisibilityFactsRepo(db)
	if err != nil {
		t.Fatalf("NewVisibilityFactsRepo: %v", err)
	}

	// LoadMany is active-only — the draft product must be absent.
	activeOnly, err := factsRepo.LoadMany(ctx, []string{prod.ID})
	if err != nil {
		t.Fatalf("LoadMany: %v", err)
	}
	if _, ok := activeOnly[prod.ID]; ok {
		t.Fatal("LoadMany must not return a draft product")
	}

	// LoadManyAnyStatus must still return it, with correct status + facts.
	anyStatus, err := factsRepo.LoadManyAnyStatus(ctx, []string{prod.ID})
	if err != nil {
		t.Fatalf("LoadManyAnyStatus: %v", err)
	}
	snap, ok := anyStatus[prod.ID]
	if !ok {
		t.Fatal("LoadManyAnyStatus must return the draft product")
	}
	if snap.Status != catalog.StatusDraft {
		t.Errorf("Status = %q, want draft", snap.Status)
	}
	if snap.Modes != catalog.DefaultVisibilityAxes() {
		t.Errorf("Modes = %+v, want defaults", snap.Modes)
	}
	if !snap.Inputs.QuantityPositive {
		t.Error("QuantityPositive = false, want true (stock = 10)")
	}
	if !snap.Inputs.HasPrice {
		t.Error("HasPrice = false, want true (price row exists)")
	}
	if !snap.Inputs.HasStoreAssignment {
		t.Error("HasStoreAssignment = false, want true (always true until store assignment exists)")
	}

	// AutoBasis resolves false (status is the only failing condition) and
	// AutoBasisFailureCodes must name exactly that.
	resolved := snap.Resolve(catalog.VisibilityOptions{})
	if resolved.VisibleInCatalog || resolved.VisibleInSearch || resolved.VisibleIndividually || resolved.Purchasable {
		t.Fatalf("resolved = %+v, want all false (draft status)", resolved)
	}
	codes := catalog.AutoBasisFailureCodes(snap.Status, snap.Inputs, catalog.VisibilityOptions{})
	if len(codes) != 1 || codes[0] != catalog.AutoBasisCodeStatusInactive {
		t.Fatalf("codes = %v, want [%q]", codes, catalog.AutoBasisCodeStatusInactive)
	}
}

func TestVisibilityFactsRepo_LoadManyAnyStatus_ActiveProductAllPass(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	ctx := context.Background()
	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	prod := mustNewProduct(t, "Active Widget", "active-widget-"+id.New()[:8])
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM products WHERE id = $1", prod.ID) })
	prod.Status = catalog.StatusActive
	if err := prodRepo.Update(ctx, &prod); err != nil {
		t.Fatalf("activate product: %v", err)
	}

	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	v := mustNewVariant(t, prod.ID, "SKU-VIS-ACTIVE-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("create variant: %v", err)
	}
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM stock WHERE variant_id = $1", v.ID)
		mustExec(t, db, "DELETE FROM prices WHERE variant_id = $1", v.ID)
		mustExec(t, db, "DELETE FROM variants WHERE id = $1", v.ID)
	})

	stockRepo, err := postgres.NewStockRepo(db)
	if err != nil {
		t.Fatalf("NewStockRepo: %v", err)
	}
	entry, err := inventory.NewStockEntry(v.ID, 3)
	if err != nil {
		t.Fatalf("NewStockEntry: %v", err)
	}
	if err := stockRepo.SetStock(ctx, &entry); err != nil {
		t.Fatalf("SetStock: %v", err)
	}

	priceRepo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}
	money := shared.MustNewMoney(500, "EUR")
	price, err := pricing.NewPrice(id.New(), v.ID, "", money)
	if err != nil {
		t.Fatalf("NewPrice: %v", err)
	}
	if err := priceRepo.Upsert(ctx, &price); err != nil {
		t.Fatalf("Upsert price: %v", err)
	}

	factsRepo, err := postgres.NewVisibilityFactsRepo(db)
	if err != nil {
		t.Fatalf("NewVisibilityFactsRepo: %v", err)
	}

	for _, loader := range []struct {
		name string
		fn   func() (map[string]catalog.VisibilitySnapshot, error)
	}{
		{"LoadMany", func() (map[string]catalog.VisibilitySnapshot, error) {
			return factsRepo.LoadMany(ctx, []string{prod.ID})
		}},
		{"LoadManyAnyStatus", func() (map[string]catalog.VisibilitySnapshot, error) {
			return factsRepo.LoadManyAnyStatus(ctx, []string{prod.ID})
		}},
	} {
		t.Run(loader.name, func(t *testing.T) {
			out, err := loader.fn()
			if err != nil {
				t.Fatalf("%s: %v", loader.name, err)
			}
			snap, ok := out[prod.ID]
			if !ok {
				t.Fatalf("%s must return the active product", loader.name)
			}
			codes := catalog.AutoBasisFailureCodes(snap.Status, snap.Inputs, catalog.VisibilityOptions{})
			if len(codes) != 0 {
				t.Fatalf("codes = %v, want none", codes)
			}
			resolved := snap.Resolve(catalog.VisibilityOptions{})
			if !resolved.VisibleInCatalog || !resolved.VisibleInSearch || !resolved.VisibleIndividually || !resolved.Purchasable {
				t.Fatalf("resolved = %+v, want all true", resolved)
			}
		})
	}
}

// TestVisibilityFactsRepo_LoadManyAnyStatus_DraftForcedVisibleNotEffective
// confirms the storefront gate still excludes a draft product even when a
// merchant forces the catalog axis visible: LoadManyAnyStatus must still
// return the snapshot (admin needs to show it), but
// catalog.EffectiveAxis(visible, draft, auto) must be false — ResolveAxis
// alone (domain-level "resolved") is not storefront truth for non-active
// products; Effective is (PR-1059 CR).
func TestVisibilityFactsRepo_LoadManyAnyStatus_DraftForcedVisibleNotEffective(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	ctx := context.Background()
	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	prod := mustNewProduct(t, "Draft Forced Visible Widget", "draft-forced-visible-"+id.New()[:8])
	// Force catalog visible while leaving status draft (NewProduct default).
	prod.VisibilityModes.Catalog = catalog.VisibilityModeVisible
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM products WHERE id = $1", prod.ID) })

	factsRepo, err := postgres.NewVisibilityFactsRepo(db)
	if err != nil {
		t.Fatalf("NewVisibilityFactsRepo: %v", err)
	}

	// Public gate (active-only) must not see the draft product at all.
	activeOnly, err := factsRepo.LoadMany(ctx, []string{prod.ID})
	if err != nil {
		t.Fatalf("LoadMany: %v", err)
	}
	if _, ok := activeOnly[prod.ID]; ok {
		t.Fatal("LoadMany must not return a draft product even with a forced-visible axis")
	}

	// Admin facts still return it so the GUI can explain the state.
	anyStatus, err := factsRepo.LoadManyAnyStatus(ctx, []string{prod.ID})
	if err != nil {
		t.Fatalf("LoadManyAnyStatus: %v", err)
	}
	snap, ok := anyStatus[prod.ID]
	if !ok {
		t.Fatal("LoadManyAnyStatus must return the draft product")
	}
	if snap.Modes.Catalog != catalog.VisibilityModeVisible {
		t.Fatalf("Modes.Catalog = %q, want visible", snap.Modes.Catalog)
	}

	auto := catalog.AutoBasis(snap.Status, snap.Inputs, catalog.VisibilityOptions{})
	if catalog.ResolveAxis(snap.Modes.Catalog, auto) != true {
		t.Fatal("ResolveAxis(visible, ...) should be true regardless of status")
	}
	if catalog.EffectiveAxis(snap.Modes.Catalog, snap.Status, auto) {
		t.Fatal("EffectiveAxis must be false for a draft product even when forced visible")
	}
}
