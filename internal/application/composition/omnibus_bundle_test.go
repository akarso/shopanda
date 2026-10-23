package composition_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/testutil"
)

// spyPriceHistory records which variant Omnibus reads. A dynamic skip must
// not call it. A fixed bundle must read the parent variant.
type spyPriceHistory struct {
	byVariant      map[string]*pricing.PriceSnapshot
	byVariantStore map[string]*pricing.PriceSnapshot
	fallback       *pricing.PriceSnapshot
	lowestCalls    int
	batchCalls     int
	queried        []string
	queriedStores  []string
}

func (m *spyPriceHistory) Record(context.Context, *pricing.PriceSnapshot) error { return nil }

func (m *spyPriceHistory) LowestSince(_ context.Context, variantID, _, storeID string, _ time.Time) (*pricing.PriceSnapshot, error) {
	m.lowestCalls++
	m.queried = append(m.queried, variantID)
	m.queriedStores = append(m.queriedStores, storeID)
	if m.byVariantStore != nil {
		return m.byVariantStore[variantID+"|"+storeID], nil
	}
	if m.byVariant != nil {
		if snap, ok := m.byVariant[variantID]; ok {
			return snap, nil
		}
	}
	return m.fallback, nil
}

func (m *spyPriceHistory) LowestSinceByVariants(ctx context.Context, variantIDs []string, currency, storeID string, since time.Time) (map[string]*pricing.PriceSnapshot, error) {
	m.batchCalls++
	return testutil.LowestSinceByVariantsFromLowest(ctx, m.LowestSince, variantIDs, currency, storeID, since)
}

func dynamicBundleProduct(explode bool) catalog.Product {
	return catalog.Product{
		ID:                "bundle",
		Name:              "Kit",
		Type:              catalog.TypeBundle,
		PricingMode:       catalog.PricingModeDynamic,
		StockMode:         catalog.StockModeDynamic,
		ExplodeOrderLines: explode,
	}
}

func fixedBundleProduct() catalog.Product {
	return catalog.Product{
		ID:                "bundle",
		Name:              "Fixed kit",
		Type:              catalog.TypeBundle,
		PricingMode:       catalog.PricingModeFixed,
		StockMode:         catalog.StockModeFixed,
		ExplodeOrderLines: false,
	}
}

func lowerParentHistory() *pricing.PriceSnapshot {
	return &pricing.PriceSnapshot{
		ID:         "snap-parent",
		VariantID:  "vb",
		Amount:     shared.MustNewMoney(2999, "EUR"),
		RecordedAt: time.Now().UTC().AddDate(0, 0, -10),
	}
}

func TestPriceIndicationStep_DynamicSkipsWhenExplodeOnOrOff(t *testing.T) {
	for _, explode := range []bool{false, true} {
		t.Run(explodeLabel(explode), func(t *testing.T) {
			history := &spyPriceHistory{fallback: lowerParentHistory()}
			prod := dynamicBundleProduct(explode)
			s := composition.NewPriceIndicationStep(
				&mockVariantRepo{variants: []catalog.Variant{{ID: "vb", ProductID: "bundle"}}},
				&mockPriceRepo{price: &pricing.Price{ID: "pr", VariantID: "vb", Amount: shared.MustNewMoney(3999, "EUR")}},
				history,
				nil,
			)
			ctx := composition.NewProductContext(&prod)
			ctx.Currency = "EUR"
			if err := s.Apply(ctx); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if len(ctx.Blocks) != 0 {
				t.Fatalf("blocks = %d, want 0 for dynamic explode=%v", len(ctx.Blocks), explode)
			}
			if history.lowestCalls != 0 || history.batchCalls != 0 {
				t.Fatalf("history reads = lowest %d batch %d, want 0", history.lowestCalls, history.batchCalls)
			}
		})
	}
}

func TestListingPriceIndicationStep_DynamicSkipsWhenExplodeOnOrOff(t *testing.T) {
	for _, explode := range []bool{false, true} {
		t.Run(explodeLabel(explode), func(t *testing.T) {
			history := &spyPriceHistory{fallback: lowerParentHistory()}
			prod := dynamicBundleProduct(explode)
			s := composition.NewListingPriceIndicationStep(
				&mockVariantRepo{variants: []catalog.Variant{{ID: "vb", ProductID: "bundle"}}},
				&mockPriceRepo{price: &pricing.Price{ID: "pr", VariantID: "vb", Amount: shared.MustNewMoney(3999, "EUR")}},
				history,
				nil,
			)
			ctx := composition.NewListingContext([]*catalog.Product{&prod})
			ctx.Currency = "EUR"
			if err := s.Apply(ctx); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if composition.PriceIndicationsFromMeta(ctx.Meta) != nil {
				t.Fatalf("listing indications = %v, want none for dynamic explode=%v", ctx.Meta, explode)
			}
			if history.lowestCalls != 0 || history.batchCalls != 0 {
				t.Fatalf("history reads = lowest %d batch %d, want 0", history.lowestCalls, history.batchCalls)
			}
		})
	}
}

func TestPriceIndicationStep_FixedBundleUsesParentHistory(t *testing.T) {
	history := &spyPriceHistory{byVariant: map[string]*pricing.PriceSnapshot{
		"vb": lowerParentHistory(),
	}}
	prod := fixedBundleProduct()
	s := composition.NewPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{{ID: "vb", ProductID: "bundle"}}},
		&mockPriceRepo{price: &pricing.Price{ID: "pr", VariantID: "vb", Amount: shared.MustNewMoney(3999, "EUR")}},
		history,
		nil,
	)
	ctx := composition.NewProductContext(&prod)
	ctx.Currency = "EUR"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(ctx.Blocks))
	}
	if ctx.Blocks[0].Data["lowest_30d_price"] != "29.99" {
		t.Fatalf("lowest_30d_price = %v, want parent history 29.99", ctx.Blocks[0].Data["lowest_30d_price"])
	}
	if len(history.queried) != 1 || history.queried[0] != "vb" {
		t.Fatalf("LowestSince variants = %v, want parent vb", history.queried)
	}
}

func TestListingPriceIndicationStep_FixedBundleUsesParentHistory(t *testing.T) {
	history := &spyPriceHistory{byVariant: map[string]*pricing.PriceSnapshot{
		"vb": lowerParentHistory(),
	}}
	prod := fixedBundleProduct()
	s := composition.NewListingPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{{ID: "vb", ProductID: "bundle"}}},
		&mockPriceRepo{price: &pricing.Price{ID: "pr", VariantID: "vb", Amount: shared.MustNewMoney(3999, "EUR")}},
		history,
		nil,
	)
	ctx := composition.NewListingContext([]*catalog.Product{&prod})
	ctx.Currency = "EUR"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	indications := composition.PriceIndicationsFromMeta(ctx.Meta)
	if indications == nil || indications["bundle"] == nil {
		t.Fatal("expected price indication for the fixed bundle")
	}
	if indications["bundle"]["lowest_30d_price"] != "29.99" {
		t.Fatalf("lowest_30d_price = %v, want parent history 29.99", indications["bundle"]["lowest_30d_price"])
	}
	if len(history.queried) != 1 || history.queried[0] != "vb" {
		t.Fatalf("LowestSince variants = %v, want parent vb", history.queried)
	}
}

func TestPriceIndicationStep_UsesStoreScopedHistory(t *testing.T) {
	history := &spyPriceHistory{byVariantStore: map[string]*pricing.PriceSnapshot{
		"v1|store-eu": {
			ID:         "snap-store",
			VariantID:  "v1",
			StoreID:    "store-eu",
			Amount:     shared.MustNewMoney(2999, "EUR"),
			RecordedAt: time.Now().UTC().AddDate(0, 0, -5),
		},
		"v1|": {
			ID:         "snap-global",
			VariantID:  "v1",
			Amount:     shared.MustNewMoney(100, "EUR"),
			RecordedAt: time.Now().UTC().AddDate(0, 0, -2),
		},
	}}
	prod := catalog.Product{ID: "p1", Name: "Widget", Type: catalog.TypeSimple, PricingMode: catalog.PricingModeFixed}
	s := composition.NewPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}}},
		&mockPriceRepo{price: &pricing.Price{ID: "pr", VariantID: "v1", Amount: shared.MustNewMoney(3999, "EUR")}},
		history,
		nil,
	)
	ctx := composition.NewProductContext(&prod)
	ctx.Currency = "EUR"
	ctx.StoreID = "store-eu"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(ctx.Blocks))
	}
	if ctx.Blocks[0].Data["lowest_30d_price"] != "29.99" {
		t.Fatalf("lowest_30d_price = %v, want store-eu history 29.99", ctx.Blocks[0].Data["lowest_30d_price"])
	}
	if len(history.queriedStores) != 1 || history.queriedStores[0] != "store-eu" {
		t.Fatalf("LowestSince stores = %v, want [store-eu]", history.queriedStores)
	}
}

func TestListingPriceIndicationStep_DynamicKitSkippedNormalKeepsIndication(t *testing.T) {
	kit := dynamicBundleProduct(true)
	normal := catalog.Product{ID: "plain", Name: "Widget", Type: catalog.TypeSimple, PricingMode: catalog.PricingModeFixed}
	history := &spyPriceHistory{byVariant: map[string]*pricing.PriceSnapshot{
		"v-plain": {
			ID:         "snap-plain",
			VariantID:  "v-plain",
			Amount:     shared.MustNewMoney(1500, "EUR"),
			RecordedAt: time.Now().UTC().AddDate(0, 0, -4),
		},
		"vb": lowerParentHistory(),
	}}
	s := composition.NewListingPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{
			{ID: "vb", ProductID: "bundle"},
			{ID: "v-plain", ProductID: "plain"},
		}},
		&variantPriceRepo{prices: map[string]*pricing.Price{
			"vb":      {ID: "pr-kit", VariantID: "vb", Amount: shared.MustNewMoney(5000, "EUR")},
			"v-plain": {ID: "pr-plain", VariantID: "v-plain", Amount: shared.MustNewMoney(3999, "EUR")},
		}},
		history,
		nil,
	)
	ctx := composition.NewListingContext([]*catalog.Product{&kit, &normal})
	ctx.Currency = "EUR"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	indications := composition.PriceIndicationsFromMeta(ctx.Meta)
	if indications == nil || indications["plain"] == nil {
		t.Fatal("expected price indication for the normal product")
	}
	if _, ok := indications["bundle"]; ok {
		t.Fatalf("dynamic kit indication = %v, want none", indications["bundle"])
	}
	if indications["plain"]["lowest_30d_price"] != "15.00" || indications["plain"]["current_price"] != "39.99" {
		t.Fatalf("plain indication = %v, want current 39.99 lowest 15.00", indications["plain"])
	}
	if len(history.queried) != 1 || history.queried[0] != "v-plain" {
		t.Fatalf("LowestSince variants = %v, want only v-plain", history.queried)
	}
}

func explodeLabel(explode bool) string {
	if explode {
		return "explode"
	}
	return "no explode"
}
