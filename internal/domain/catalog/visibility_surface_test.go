package catalog_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestVisibility_AllowedOn(t *testing.T) {
	v := catalog.Visibility{
		VisibleInCatalog:    true,
		VisibleInSearch:     false,
		VisibleIndividually: true,
		Purchasable:         false,
	}
	if !v.AllowedOn(catalog.VisibilitySurfaceCatalog) {
		t.Fatal("catalog")
	}
	if v.AllowedOn(catalog.VisibilitySurfaceSearch) {
		t.Fatal("search")
	}
	if !v.AllowedOn(catalog.VisibilitySurfaceIndividually) {
		t.Fatal("individually")
	}
	if v.AllowedOn(catalog.VisibilitySurfacePurchasable) {
		t.Fatal("purchasable")
	}
	if v.AllowedOn(catalog.VisibilitySurface("unknown")) {
		t.Fatal("unknown must fail closed")
	}
}

func TestAllowProduct_BypassAndGate(t *testing.T) {
	p, err := catalog.NewProduct("p1", "P", "p")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	p.Status = catalog.StatusActive
	p.VisibilityModes.Catalog = catalog.VisibilityModeHidden

	sellable := catalog.VisibilityInputs{
		QuantityPositive:   true,
		HasPrice:           true,
		HasStoreAssignment: true,
	}
	opts := catalog.VisibilityOptions{}

	if catalog.AllowProduct(context.Background(), &p, catalog.VisibilitySurfaceCatalog, sellable, opts) {
		t.Fatal("hidden catalog axis must deny on public ctx")
	}
	admin := catalog.WithBypassProductVisibility(context.Background())
	if !catalog.AllowProduct(admin, &p, catalog.VisibilitySurfaceCatalog, sellable, opts) {
		t.Fatal("admin bypass must allow")
	}
	if catalog.AllowProduct(context.Background(), nil, catalog.VisibilitySurfaceCatalog, sellable, opts) {
		t.Fatal("nil product")
	}
}

func TestAllowProduct_DraftForcedVisible_StillPassesAxisGate(t *testing.T) {
	// Documents the two-gate contract: AllowProduct is axis-only; Status is
	// enforced separately (repo scope / ActiveForPurchase). A draft with
	// forced-visible must not be mistaken for "storefront-safe".
	p, err := catalog.NewProduct("p1", "P", "p")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	// NewProduct leaves Status=draft.
	p.VisibilityModes = catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeVisible,
		Search:       catalog.VisibilityModeVisible,
		Individually: catalog.VisibilityModeVisible,
		Purchasable:  catalog.VisibilityModeVisible,
	}
	if !catalog.AllowProduct(context.Background(), &p, catalog.VisibilitySurfaceCatalog, catalog.VisibilityInputs{}, catalog.VisibilityOptions{}) {
		t.Fatal("axis gate allows forced-visible draft; status must still be filtered elsewhere")
	}
	if catalog.ActiveForPurchase(&p) {
		t.Fatal("ActiveForPurchase must still reject draft")
	}
}

func TestAllowProduct_NilFacts(t *testing.T) {
	p, err := catalog.NewProduct("p1", "P", "p")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	p.Status = catalog.StatusActive
	opts := catalog.VisibilityOptions{}
	empty := catalog.VisibilityInputs{}

	if catalog.AllowProduct(context.Background(), &p, catalog.VisibilitySurfaceCatalog, empty, opts) {
		t.Fatal("auto + empty facts must deny")
	}

	p.VisibilityModes.Catalog = catalog.VisibilityModeVisible
	if !catalog.AllowProduct(context.Background(), &p, catalog.VisibilitySurfaceCatalog, empty, opts) {
		t.Fatal("forced visible + empty facts must allow on axis gate")
	}
}

func TestFilterProducts(t *testing.T) {
	hidden, err := catalog.NewProduct("h1", "Hidden", "hidden")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	hidden.Status = catalog.StatusActive
	hidden.VisibilityModes.Catalog = catalog.VisibilityModeHidden

	shown, err := catalog.NewProduct("s1", "Shown", "shown")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	shown.Status = catalog.StatusActive

	facts := func(catalog.Product) catalog.VisibilityInputs {
		return catalog.VisibilityInputs{
			QuantityPositive:   true,
			HasPrice:           true,
			HasStoreAssignment: true,
		}
	}
	products := []catalog.Product{hidden, shown}
	got := catalog.FilterProducts(context.Background(), products, catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{})
	if len(got) != 1 || got[0].ID != "s1" {
		t.Fatalf("got %+v, want only s1", got)
	}

	admin := catalog.WithBypassProductVisibility(context.Background())
	all := catalog.FilterProducts(admin, products, catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{})
	if len(all) != 2 {
		t.Fatalf("admin filter len = %d, want 2", len(all))
	}
	all[0].Name = "mutated"
	if products[0].Name == "mutated" {
		t.Fatal("FilterProducts must not alias input slice on bypass")
	}
}

func TestFilterProducts_NilFactsUnknownSurfaceOrder(t *testing.T) {
	a, err := catalog.NewProduct("a", "A", "a")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	a.Status = catalog.StatusActive
	a.VisibilityModes.Catalog = catalog.VisibilityModeVisible

	b, err := catalog.NewProduct("b", "B", "b")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	b.Status = catalog.StatusActive // auto + nil facts → deny

	products := []catalog.Product{a, b}
	got := catalog.FilterProducts(context.Background(), products, catalog.VisibilitySurfaceCatalog, nil, catalog.VisibilityOptions{})
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("nil facts: got %+v, want only forced-visible a", got)
	}

	none := catalog.FilterProducts(context.Background(), products, catalog.VisibilitySurface("nope"), nil, catalog.VisibilityOptions{})
	if len(none) != 0 {
		t.Fatalf("unknown surface: got %d, want 0", len(none))
	}

	// Preserve relative order when both pass.
	b.VisibilityModes.Catalog = catalog.VisibilityModeVisible
	products = []catalog.Product{a, b}
	ordered := catalog.FilterProducts(context.Background(), products, catalog.VisibilitySurfaceCatalog, nil, catalog.VisibilityOptions{})
	if len(ordered) != 2 || ordered[0].ID != "a" || ordered[1].ID != "b" {
		t.Fatalf("order = %+v", ordered)
	}
}
