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

// --- mock for PriceHistoryRepository ---

type mockPriceHistoryRepo struct {
	snapshot *pricing.PriceSnapshot
	err      error
}

func (m *mockPriceHistoryRepo) Record(_ context.Context, _ *pricing.PriceSnapshot) error {
	return nil
}

func (m *mockPriceHistoryRepo) LowestSince(_ context.Context, _, _, _ string, _ time.Time) (*pricing.PriceSnapshot, error) {
	return m.snapshot, m.err
}

// --- tests ---

func (m *mockPriceHistoryRepo) LowestSinceByVariants(ctx context.Context, variantIDs []string, currency, storeID string, since time.Time) (map[string]*pricing.PriceSnapshot, error) {
	return testutil.LowestSinceByVariantsFromLowest(ctx, m.LowestSince, variantIDs, currency, storeID, since)
}

func TestPriceIndicationStep_Name(t *testing.T) {
	s := composition.NewPriceIndicationStep(&mockVariantRepo{}, &mockPriceRepo{}, &mockPriceHistoryRepo{}, nil)
	if s.Name() != "price_indication" {
		t.Errorf("Name() = %q, want price_indication", s.Name())
	}
}

func TestPriceIndicationStep_NilProduct(t *testing.T) {
	s := composition.NewPriceIndicationStep(&mockVariantRepo{}, &mockPriceRepo{}, &mockPriceHistoryRepo{}, nil)
	ctx := composition.NewProductContext(nil)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Error("expected no blocks for nil product")
	}
}

func TestPriceIndicationStep_NoVariants(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget"}
	s := composition.NewPriceIndicationStep(
		&mockVariantRepo{variants: nil},
		&mockPriceRepo{},
		&mockPriceHistoryRepo{},
		nil,
	)
	ctx := composition.NewProductContext(&prod)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Error("expected no blocks when no variants")
	}
}

func TestPriceIndicationStep_NoHistory(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget"}
	currentAmount := shared.MustNewMoney(2999, "EUR")
	currentPrice := &pricing.Price{ID: "pr1", VariantID: "v1", Amount: currentAmount}

	s := composition.NewPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}}},
		&mockPriceRepo{price: currentPrice},
		&mockPriceHistoryRepo{snapshot: nil},
		nil,
	)
	ctx := composition.NewProductContext(&prod)
	ctx.Currency = "EUR"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Error("expected no blocks when no history")
	}
}

func TestPriceIndicationStep_LowerHistorical(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget"}
	currentAmount := shared.MustNewMoney(3999, "EUR")
	currentPrice := &pricing.Price{ID: "pr1", VariantID: "v1", Amount: currentAmount}

	lowestAmount := shared.MustNewMoney(2999, "EUR")
	lowestSnap := &pricing.PriceSnapshot{
		ID:         "snap1",
		VariantID:  "v1",
		Amount:     lowestAmount,
		RecordedAt: time.Now().UTC().AddDate(0, 0, -10),
	}

	s := composition.NewPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}}},
		&mockPriceRepo{price: currentPrice},
		&mockPriceHistoryRepo{snapshot: lowestSnap},
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
	blk := ctx.Blocks[0]
	if blk.Type != "price_indication" {
		t.Errorf("block type = %q, want price_indication", blk.Type)
	}
	if blk.Data["current_price"] != "39.99" {
		t.Errorf("current_price = %v, want 39.99", blk.Data["current_price"])
	}
	if blk.Data["lowest_30d_price"] != "29.99" {
		t.Errorf("lowest_30d_price = %v, want 29.99", blk.Data["lowest_30d_price"])
	}
	if blk.Data["currency"] != "EUR" {
		t.Errorf("currency = %v, want EUR", blk.Data["currency"])
	}
	if blk.Data["recorded_at"] == nil || blk.Data["recorded_at"] == "" {
		t.Error("recorded_at should be non-empty")
	}
}

func TestPriceIndicationStep_SamePrice(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget"}
	amount := shared.MustNewMoney(2999, "EUR")
	currentPrice := &pricing.Price{ID: "pr1", VariantID: "v1", Amount: amount}

	snap := &pricing.PriceSnapshot{
		ID:         "snap1",
		VariantID:  "v1",
		Amount:     amount,
		RecordedAt: time.Now().UTC().AddDate(0, 0, -5),
	}

	s := composition.NewPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}}},
		&mockPriceRepo{price: currentPrice},
		&mockPriceHistoryRepo{snapshot: snap},
		nil,
	)
	ctx := composition.NewProductContext(&prod)
	ctx.Currency = "EUR"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Error("expected no block when lowest equals current price")
	}
}

func TestPriceIndicationStep_HigherHistorical(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget"}
	currentAmount := shared.MustNewMoney(1999, "EUR")
	currentPrice := &pricing.Price{ID: "pr1", VariantID: "v1", Amount: currentAmount}

	higherSnap := &pricing.PriceSnapshot{
		ID:         "snap1",
		VariantID:  "v1",
		Amount:     shared.MustNewMoney(2999, "EUR"),
		RecordedAt: time.Now().UTC().AddDate(0, 0, -15),
	}

	s := composition.NewPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}}},
		&mockPriceRepo{price: currentPrice},
		&mockPriceHistoryRepo{snapshot: higherSnap},
		nil,
	)
	ctx := composition.NewProductContext(&prod)
	ctx.Currency = "EUR"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Error("expected no block when lowest historical is higher than current")
	}
}

type stubOmnibusConfig map[string]interface{}

func (s stubOmnibusConfig) Get(_ context.Context, key string) (interface{}, error) {
	if s == nil {
		return nil, nil
	}
	v, ok := s[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func TestPriceIndicationStep_DisabledByConfig(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget"}
	currentAmount := shared.MustNewMoney(3999, "EUR")
	currentPrice := &pricing.Price{ID: "pr1", VariantID: "v1", Amount: currentAmount}
	lowestSnap := &pricing.PriceSnapshot{
		ID:         "snap1",
		VariantID:  "v1",
		Amount:     shared.MustNewMoney(2999, "EUR"),
		RecordedAt: time.Now().UTC().AddDate(0, 0, -10),
	}

	cfg := stubOmnibusConfig{"store::store-eu::legal.omnibus_enabled": false}
	s := composition.NewPriceIndicationStep(
		&mockVariantRepo{variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}}},
		&mockPriceRepo{price: currentPrice},
		&mockPriceHistoryRepo{snapshot: lowestSnap},
		cfg,
	)
	ctx := composition.NewProductContext(&prod)
	ctx.Currency = "EUR"
	ctx.StoreID = "store-eu"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Fatal("expected no blocks when omnibus disabled")
	}
}
