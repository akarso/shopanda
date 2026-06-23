package composition_test

import (
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
