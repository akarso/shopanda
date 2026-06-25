package composition_test

import (
	"context"
	"github.com/akarso/shopanda/internal/testutil"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

func TestListingPriceIndicationStep_PopulatesMeta(t *testing.T) {
	currentAmount := shared.MustNewMoney(3999, "EUR")
	currentPrice := &pricing.Price{ID: "pr1", VariantID: "v1", Amount: currentAmount}
	lowestSnap := &pricing.PriceSnapshot{
		ID:         "snap1",
		VariantID:  "v1",
		Amount:     shared.MustNewMoney(2999, "EUR"),
		RecordedAt: time.Now().UTC().AddDate(0, 0, -10),
	}

	s := composition.NewListingPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}}},
		&mockPriceRepo{price: currentPrice},
		&mockPriceHistoryRepo{snapshot: lowestSnap},
		nil,
	)
	prod := &catalog.Product{ID: "p1", Name: "Widget"}
	ctx := composition.NewListingContext([]*catalog.Product{prod})
	ctx.Currency = "EUR"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	indications := composition.PriceIndicationsFromMeta(ctx.Meta)
	if indications == nil || indications["p1"] == nil {
		t.Fatal("expected price indication for p1")
	}
	if indications["p1"]["lowest_30d_price"] != "29.99" {
		t.Fatalf("lowest_30d_price = %v", indications["p1"]["lowest_30d_price"])
	}
}

func TestListingPriceIndicationStep_UsesLowestPriceVariant(t *testing.T) {
	expensive := shared.MustNewMoney(5000, "EUR")
	cheapCurrent := shared.MustNewMoney(3999, "EUR")
	lowestSnap := &pricing.PriceSnapshot{
		ID:         "snap-cheap",
		VariantID:  "v-cheap",
		Amount:     shared.MustNewMoney(2999, "EUR"),
		RecordedAt: time.Now().UTC().AddDate(0, 0, -10),
	}

	s := composition.NewListingPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{
			{ID: "v-expensive", ProductID: "p1"},
			{ID: "v-cheap", ProductID: "p1"},
		}},
		&variantPriceRepo{prices: map[string]*pricing.Price{
			"v-expensive": {VariantID: "v-expensive", Amount: expensive},
			"v-cheap":     {VariantID: "v-cheap", Amount: cheapCurrent},
		}},
		&mockPriceHistoryRepo{snapshot: lowestSnap},
		nil,
	)
	prod := &catalog.Product{ID: "p1", Name: "Widget"}
	ctx := composition.NewListingContext([]*catalog.Product{prod})
	ctx.Currency = "EUR"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	indications := composition.PriceIndicationsFromMeta(ctx.Meta)
	if indications == nil || indications["p1"] == nil {
		t.Fatal("expected price indication for cheapest variant")
	}
	if indications["p1"]["current_price"] != "39.99" {
		t.Fatalf("current_price = %v, want 39.99 (lowest variant)", indications["p1"]["current_price"])
	}
}

type variantPriceRepo struct {
	prices map[string]*pricing.Price
}

func (m *variantPriceRepo) FindByVariantCurrencyAndStore(_ context.Context, variantID, _, _ string) (*pricing.Price, error) {
	if m == nil {
		return nil, nil
	}
	return m.prices[variantID], nil
}
func (m *variantPriceRepo) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*pricing.Price, error) {
	return testutil.FindByVariantsCurrencyAndStoreFromFind(ctx, m.FindByVariantCurrencyAndStore, variantIDs, currency, storeID)
}

func (m *variantPriceRepo) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}
func (m *variantPriceRepo) Upsert(_ context.Context, _ *pricing.Price) error { return nil }
func (m *variantPriceRepo) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}

func TestListingPriceIndicationStep_DisabledByConfig(t *testing.T) {
	cfg := stubOmnibusConfig{"store::store-eu::legal.omnibus_enabled": false}
	s := composition.NewListingPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}}},
		&mockPriceRepo{price: &pricing.Price{VariantID: "v1", Amount: shared.MustNewMoney(1000, "EUR")}},
		&mockPriceHistoryRepo{snapshot: &pricing.PriceSnapshot{VariantID: "v1", Amount: shared.MustNewMoney(500, "EUR"), RecordedAt: time.Now().UTC()}},
		cfg,
	)
	prod := &catalog.Product{ID: "p1", Name: "Widget"}
	ctx := composition.NewListingContext([]*catalog.Product{prod})
	ctx.StoreID = "store-eu"
	ctx.Currency = "EUR"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if composition.PriceIndicationsFromMeta(ctx.Meta) != nil {
		t.Fatal("expected no indications when disabled")
	}
}

type batchCallVariantRepo struct {
	mockVariantRepo
	listByProductIDsCalls int
}

func (m *batchCallVariantRepo) ListByProductIDs(ctx context.Context, productIDs []string, limitPerProduct int) (map[string][]catalog.Variant, error) {
	m.listByProductIDsCalls++
	out := make(map[string][]catalog.Variant, len(productIDs))
	for _, pid := range productIDs {
		for _, v := range m.variants {
			if v.ProductID != pid {
				continue
			}
			list := out[pid]
			if len(list) >= limitPerProduct {
				break
			}
			out[pid] = append(list, v)
		}
	}
	return out, nil
}

type batchCallPriceRepo struct {
	mockPriceRepo
	batchCalls        int
	lastBatchCurrency string
}

func (m *batchCallPriceRepo) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*pricing.Price, error) {
	m.batchCalls++
	m.lastBatchCurrency = currency
	return testutil.FindByVariantsCurrencyAndStoreFromFind(ctx, m.FindByVariantCurrencyAndStore, variantIDs, currency, storeID)
}

type batchCallHistoryRepo struct {
	mockPriceHistoryRepo
	batchCalls        int
	lastBatchCurrency string
}

func (m *batchCallHistoryRepo) LowestSinceByVariants(ctx context.Context, variantIDs []string, currency, storeID string, since time.Time) (map[string]*pricing.PriceSnapshot, error) {
	m.batchCalls++
	m.lastBatchCurrency = currency
	return testutil.LowestSinceByVariantsFromLowest(ctx, m.LowestSince, variantIDs, currency, storeID, since)
}

func TestListingPriceIndicationStep_UsesBatchReads(t *testing.T) {
	runBatchReadTest := func(t *testing.T, currency string) {
		t.Helper()
		variants := &batchCallVariantRepo{mockVariantRepo: mockVariantRepo{variants: []catalog.Variant{
			{ID: "v1", ProductID: "p1"},
			{ID: "v2", ProductID: "p2"},
		}}}
		prices := &batchCallPriceRepo{mockPriceRepo: mockPriceRepo{price: &pricing.Price{VariantID: "v1", Amount: shared.MustNewMoney(3999, "EUR")}}}
		history := &batchCallHistoryRepo{mockPriceHistoryRepo: mockPriceHistoryRepo{snapshot: &pricing.PriceSnapshot{
			VariantID:  "v1",
			Amount:     shared.MustNewMoney(2999, "EUR"),
			RecordedAt: time.Now().UTC().AddDate(0, 0, -10),
		}}}

		s := composition.NewListingPriceIndicationStep(variants, prices, history, nil)
		ctx := composition.NewListingContext([]*catalog.Product{
			{ID: "p1", Name: "One"},
			{ID: "p2", Name: "Two"},
		})
		ctx.Currency = currency
		if err := s.Apply(ctx); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if variants.listByProductIDsCalls != 1 {
			t.Fatalf("ListByProductIDs calls = %d, want 1", variants.listByProductIDsCalls)
		}
		if prices.batchCalls != 1 {
			t.Fatalf("FindByVariantsCurrencyAndStore calls = %d, want 1", prices.batchCalls)
		}
		if history.batchCalls != 1 {
			t.Fatalf("LowestSinceByVariants calls = %d, want 1", history.batchCalls)
		}
		wantCurrency := currency
		if wantCurrency == "" {
			wantCurrency = "EUR"
		}
		if prices.lastBatchCurrency != wantCurrency {
			t.Fatalf("batch price currency = %q, want %q", prices.lastBatchCurrency, wantCurrency)
		}
		if history.lastBatchCurrency != wantCurrency {
			t.Fatalf("batch history currency = %q, want %q", history.lastBatchCurrency, wantCurrency)
		}
	}

	t.Run("explicit currency", func(t *testing.T) {
		runBatchReadTest(t, "EUR")
	})
	t.Run("empty currency defaults to EUR", func(t *testing.T) {
		runBatchReadTest(t, "")
	})
}
