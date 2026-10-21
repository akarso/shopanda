package postgres_test

import (
	"context"
	"fmt"
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
	t.Cleanup(func() { deleteProductByID(t, db, prod.ID) })

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
	t.Cleanup(func() { deleteProductByID(t, db, prod.ID) })
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
	t.Cleanup(func() { deleteProductByID(t, db, prod.ID) })

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

func TestVisibilityFactsRepo_DynamicBundleHasPriceFromRequiredComponents(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	ctx := context.Background()

	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	priceRepo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}
	componentRepo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	factsRepo, err := postgres.NewVisibilityFactsRepo(db)
	if err != nil {
		t.Fatalf("NewVisibilityFactsRepo: %v", err)
	}

	bundle := mustNewProduct(t, "Dynamic Kit", "dyn-kit-"+id.New()[:8])
	bundle.Type = catalog.TypeBundle
	bundle.PricingMode = catalog.PricingModeDynamic
	bundle.Status = catalog.StatusActive
	if err := prodRepo.Create(ctx, &bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	component := mustNewProduct(t, "Piece", "piece-"+id.New()[:8])
	component.Status = catalog.StatusActive
	if err := prodRepo.Create(ctx, &component); err != nil {
		t.Fatalf("create component: %v", err)
	}
	variant := mustNewVariant(t, component.ID, "SKU-PIECE-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &variant); err != nil {
		t.Fatalf("create variant: %v", err)
	}
	zero, err := pricing.NewPrice(id.New(), variant.ID, "", shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("NewPrice: %v", err)
	}
	if err := priceRepo.Upsert(ctx, &zero); err != nil {
		t.Fatalf("upsert 0: %v", err)
	}
	link, err := catalog.NewBundleComponent(bundle.ID, component.ID, 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	if err := componentRepo.Upsert(ctx, link); err != nil {
		t.Fatalf("upsert component: %v", err)
	}

	snap, err := factsRepo.LoadMany(ctx, []string{bundle.ID})
	if err != nil {
		t.Fatalf("LoadMany: %v", err)
	}
	got, ok := snap[bundle.ID]
	if !ok {
		t.Fatal("dynamic bundle missing from facts")
	}
	if !got.Inputs.HasPrice {
		t.Fatal("HasPrice = false, want true (required component amount 0 still counts)")
	}

	fixed := mustNewProduct(t, "Fixed Kit", "fix-kit-"+id.New()[:8])
	fixed.Type = catalog.TypeBundle
	fixed.PricingMode = catalog.PricingModeFixed
	fixed.Status = catalog.StatusActive
	if err := prodRepo.Create(ctx, &fixed); err != nil {
		t.Fatalf("create fixed bundle: %v", err)
	}
	fixedLink, err := catalog.NewBundleComponent(fixed.ID, component.ID, 1)
	if err != nil {
		t.Fatalf("fixed link: %v", err)
	}
	if err := componentRepo.Upsert(ctx, fixedLink); err != nil {
		t.Fatalf("upsert fixed link: %v", err)
	}
	fixedSnap, err := factsRepo.LoadMany(ctx, []string{fixed.ID})
	if err != nil {
		t.Fatalf("LoadMany fixed: %v", err)
	}
	if fixedSnap[fixed.ID].Inputs.HasPrice {
		t.Fatal("fixed bundle HasPrice = true without its own price row")
	}
}

func TestVisibilityFactsRepo_DynamicBundleHasPriceFailClosed(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	ctx := context.Background()

	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	priceRepo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}
	componentRepo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	factsRepo, err := postgres.NewVisibilityFactsRepo(db)
	if err != nil {
		t.Fatalf("NewVisibilityFactsRepo: %v", err)
	}

	hasPrice := func(t *testing.T, productID string) bool {
		t.Helper()
		snap, err := factsRepo.LoadMany(ctx, []string{productID})
		if err != nil {
			t.Fatalf("LoadMany: %v", err)
		}
		got, ok := snap[productID]
		if !ok {
			t.Fatalf("product %s missing from facts", productID)
		}
		return got.Inputs.HasPrice
	}
	newBundle := func(t *testing.T, slug string) catalog.Product {
		t.Helper()
		bundle := mustNewProduct(t, "Dynamic Kit", slug)
		bundle.Type = catalog.TypeBundle
		bundle.PricingMode = catalog.PricingModeDynamic
		bundle.Status = catalog.StatusActive
		if err := prodRepo.Create(ctx, &bundle); err != nil {
			t.Fatalf("create bundle: %v", err)
		}
		return bundle
	}
	link := func(t *testing.T, bundleID, componentID string) {
		t.Helper()
		row, err := catalog.NewBundleComponent(bundleID, componentID, 1)
		if err != nil {
			t.Fatalf("NewBundleComponent: %v", err)
		}
		if err := componentRepo.Upsert(ctx, row); err != nil {
			t.Fatalf("upsert component: %v", err)
		}
	}

	t.Run("store-only USD", func(t *testing.T) {
		bundle := newBundle(t, "dyn-store-"+id.New()[:8])
		component := mustNewProduct(t, "Piece", "piece-store-"+id.New()[:8])
		component.Status = catalog.StatusActive
		if err := prodRepo.Create(ctx, &component); err != nil {
			t.Fatalf("create component: %v", err)
		}
		variant := mustNewVariant(t, component.ID, "SKU-STORE-"+id.New()[:8])
		if err := variantRepo.Create(ctx, &variant); err != nil {
			t.Fatalf("create variant: %v", err)
		}
		storeOnly, err := pricing.NewPrice(id.New(), variant.ID, "store-us", shared.MustNewMoney(100, "USD"))
		if err != nil {
			t.Fatalf("NewPrice: %v", err)
		}
		if err := priceRepo.Upsert(ctx, &storeOnly); err != nil {
			t.Fatalf("upsert store price: %v", err)
		}
		link(t, bundle.ID, component.ID)
		if hasPrice(t, bundle.ID) {
			t.Fatal("HasPrice = true, want false for a store-only USD price")
		}

		globalUSD, err := pricing.NewPrice(id.New(), variant.ID, "", shared.MustNewMoney(100, "USD"))
		if err != nil {
			t.Fatalf("NewPrice global: %v", err)
		}
		if err := priceRepo.Upsert(ctx, &globalUSD); err != nil {
			t.Fatalf("upsert global USD: %v", err)
		}
		if !hasPrice(t, bundle.ID) {
			t.Fatal("HasPrice = false, want true for a global USD row (currency-agnostic)")
		}
	})

	t.Run("two variants", func(t *testing.T) {
		bundle := newBundle(t, "dyn-multi-"+id.New()[:8])
		component := mustNewProduct(t, "Piece", "piece-multi-"+id.New()[:8])
		component.Status = catalog.StatusActive
		if err := prodRepo.Create(ctx, &component); err != nil {
			t.Fatalf("create component: %v", err)
		}
		for _, sku := range []string{"SKU-A-", "SKU-B-"} {
			variant := mustNewVariant(t, component.ID, sku+id.New()[:8])
			if err := variantRepo.Create(ctx, &variant); err != nil {
				t.Fatalf("create variant: %v", err)
			}
			row, err := pricing.NewPrice(id.New(), variant.ID, "", shared.MustNewMoney(10, "EUR"))
			if err != nil {
				t.Fatalf("NewPrice: %v", err)
			}
			if err := priceRepo.Upsert(ctx, &row); err != nil {
				t.Fatalf("upsert: %v", err)
			}
		}
		link(t, bundle.ID, component.ID)
		if hasPrice(t, bundle.ID) {
			t.Fatal("HasPrice = true, want false when a required component has two variants")
		}
	})

	t.Run("archived component", func(t *testing.T) {
		bundle := newBundle(t, "dyn-arch-"+id.New()[:8])
		component := mustNewProduct(t, "Piece", "piece-arch-"+id.New()[:8])
		component.Status = catalog.StatusArchived
		if err := prodRepo.Create(ctx, &component); err != nil {
			t.Fatalf("create component: %v", err)
		}
		variant := mustNewVariant(t, component.ID, "SKU-ARCH-"+id.New()[:8])
		if err := variantRepo.Create(ctx, &variant); err != nil {
			t.Fatalf("create variant: %v", err)
		}
		row, err := pricing.NewPrice(id.New(), variant.ID, "", shared.MustNewMoney(10, "EUR"))
		if err != nil {
			t.Fatalf("NewPrice: %v", err)
		}
		if err := priceRepo.Upsert(ctx, &row); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		link(t, bundle.ID, component.ID)
		if hasPrice(t, bundle.ID) {
			t.Fatal("HasPrice = true, want false for an archived required component")
		}
	})

	t.Run("non-leaf component", func(t *testing.T) {
		bundle := newBundle(t, "dyn-leaf-"+id.New()[:8])
		component := mustNewProduct(t, "Group", "piece-group-"+id.New()[:8])
		component.Type = catalog.TypeGrouped
		component.Status = catalog.StatusActive
		if err := prodRepo.Create(ctx, &component); err != nil {
			t.Fatalf("create component: %v", err)
		}
		variant := mustNewVariant(t, component.ID, "SKU-GROUP-"+id.New()[:8])
		if err := variantRepo.Create(ctx, &variant); err != nil {
			t.Fatalf("create variant: %v", err)
		}
		row, err := pricing.NewPrice(id.New(), variant.ID, "", shared.MustNewMoney(10, "EUR"))
		if err != nil {
			t.Fatalf("NewPrice: %v", err)
		}
		if err := priceRepo.Upsert(ctx, &row); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		link(t, bundle.ID, component.ID)
		if hasPrice(t, bundle.ID) {
			t.Fatal("HasPrice = true, want false for a non-leaf required component")
		}
	})

	t.Run("grouped parent", func(t *testing.T) {
		group := mustNewProduct(t, "Group", "group-price-"+id.New()[:8])
		group.Type = catalog.TypeGrouped
		group.Status = catalog.StatusActive
		if err := prodRepo.Create(ctx, &group); err != nil {
			t.Fatalf("create group: %v", err)
		}
		variant := mustNewVariant(t, group.ID, "SKU-GPARENT-"+id.New()[:8])
		if err := variantRepo.Create(ctx, &variant); err != nil {
			t.Fatalf("create variant: %v", err)
		}
		row, err := pricing.NewPrice(id.New(), variant.ID, "", shared.MustNewMoney(10, "EUR"))
		if err != nil {
			t.Fatalf("NewPrice: %v", err)
		}
		if err := priceRepo.Upsert(ctx, &row); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if hasPrice(t, group.ID) {
			t.Fatal("HasPrice = true, want false for a grouped parent with a leftover price")
		}
	})

	t.Run("more than max required components", func(t *testing.T) {
		bundle := newBundle(t, "dyn-cap-"+id.New()[:8])
		for i := 0; i < catalog.MaxBundleRequiredComponents+1; i++ {
			component := mustNewProduct(t, "Piece", fmt.Sprintf("piece-cap-%s-%d", id.New()[:8], i))
			component.Status = catalog.StatusActive
			if err := prodRepo.Create(ctx, &component); err != nil {
				t.Fatalf("create component: %v", err)
			}
			variant := mustNewVariant(t, component.ID, fmt.Sprintf("SKU-CAP-%s-%d", id.New()[:8], i))
			if err := variantRepo.Create(ctx, &variant); err != nil {
				t.Fatalf("create variant: %v", err)
			}
			row, err := pricing.NewPrice(id.New(), variant.ID, "", shared.MustNewMoney(10, "EUR"))
			if err != nil {
				t.Fatalf("NewPrice: %v", err)
			}
			if err := priceRepo.Upsert(ctx, &row); err != nil {
				t.Fatalf("upsert: %v", err)
			}
			link(t, bundle.ID, component.ID)
		}
		if hasPrice(t, bundle.ID) {
			t.Fatal("HasPrice = true, want false when required components exceed the cap")
		}
	})
}
