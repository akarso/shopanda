package composition_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// --- mocks ---

type mockVariantRepo struct {
	variants []catalog.Variant
	err      error
}

func (m *mockVariantRepo) FindByID(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return m.variants, m.err
}
func (m *mockVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *mockVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }

type mockPriceRepo struct {
	price *pricing.Price
	err   error
}

func (m *mockPriceRepo) FindByVariantAndCurrency(_ context.Context, _, _ string) (*pricing.Price, error) {
	return m.price, m.err
}
func (m *mockPriceRepo) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}
func (m *mockPriceRepo) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}
func (m *mockPriceRepo) Upsert(_ context.Context, _ *pricing.Price) error { return nil }

type mockStockRepo struct {
	entry inventory.StockEntry
	err   error
}

func (m *mockStockRepo) GetStock(_ context.Context, _ string) (inventory.StockEntry, error) {
	return m.entry, m.err
}
func (m *mockStockRepo) SetStock(_ context.Context, _ *inventory.StockEntry) error { return nil }
func (m *mockStockRepo) ListStock(_ context.Context, _, _ int) ([]inventory.StockEntry, error) {
	return nil, nil
}

// --- tests ---

func TestJSONLDProductStep_Name(t *testing.T) {
	s := composition.NewJSONLDProductStep(&mockVariantRepo{}, &mockPriceRepo{}, &mockStockRepo{})
	if s.Name() != "seo_jsonld" {
		t.Errorf("Name() = %q, want seo_jsonld", s.Name())
	}
}

func TestJSONLDProductStep_NilProduct(t *testing.T) {
	s := composition.NewJSONLDProductStep(&mockVariantRepo{}, &mockPriceRepo{}, &mockStockRepo{})
	ctx := composition.NewProductContext(nil)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Error("expected no blocks for nil product")
	}
}

func TestJSONLDProductStep_BasicProduct(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget", Description: "Desc", Slug: "widget"}
	s := composition.NewJSONLDProductStep(
		&mockVariantRepo{err: errors.New("no variants")},
		&mockPriceRepo{},
		&mockStockRepo{},
	)
	ctx := composition.NewProductContext(&prod)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(ctx.Blocks))
	}
	if ctx.Blocks[0].Type != "jsonld" {
		t.Errorf("block type = %q, want jsonld", ctx.Blocks[0].Type)
	}
	ld, ok := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	if !ok {
		t.Fatal("jsonld data not a map")
	}
	if ld["@type"] != "Product" {
		t.Errorf("@type = %v, want Product", ld["@type"])
	}
	if ld["name"] != "Widget" {
		t.Errorf("name = %v, want Widget", ld["name"])
	}
	if ld["url"] != "/products/widget" {
		t.Errorf("url = %v, want /products/widget", ld["url"])
	}
	if ld["offers"] != nil {
		t.Error("expected no offers when variant lookup fails")
	}
}

func TestJSONLDProductStep_WithOffer(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget", Slug: "widget"}
	v, _ := catalog.NewVariant("v1", "p1", "SKU-1")
	amount := shared.MustNewMoney(2999, "USD")
	price := pricing.Price{ID: "pr1", VariantID: "v1", Amount: amount}
	stock, _ := inventory.NewStockEntry("v1", 5)

	s := composition.NewJSONLDProductStep(
		&mockVariantRepo{variants: []catalog.Variant{v}},
		&mockPriceRepo{price: &price},
		&mockStockRepo{entry: stock},
	)
	ctx := composition.NewProductContext(&prod)
	ctx.Currency = "USD"
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(ctx.Blocks))
	}
	ld := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	offers, ok := ld["offers"].(map[string]interface{})
	if !ok {
		t.Fatal("offers not a map")
	}
	if offers["@type"] != "Offer" {
		t.Errorf("offer @type = %v, want Offer", offers["@type"])
	}
	if offers["price"] != "29.99" {
		t.Errorf("price = %v, want 29.99", offers["price"])
	}
	if offers["priceCurrency"] != "USD" {
		t.Errorf("priceCurrency = %v, want USD", offers["priceCurrency"])
	}
	if offers["availability"] != "https://schema.org/InStock" {
		t.Errorf("availability = %v, want InStock", offers["availability"])
	}
}

func TestJSONLDProductStep_OutOfStock(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget", Slug: "widget"}
	v, _ := catalog.NewVariant("v1", "p1", "SKU-1")
	stock, _ := inventory.NewStockEntry("v1", 0)

	s := composition.NewJSONLDProductStep(
		&mockVariantRepo{variants: []catalog.Variant{v}},
		&mockPriceRepo{err: errors.New("no price")},
		&mockStockRepo{entry: stock},
	)
	ctx := composition.NewProductContext(&prod)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ld := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	offers := ld["offers"].(map[string]interface{})
	if offers["availability"] != "https://schema.org/OutOfStock" {
		t.Errorf("availability = %v, want OutOfStock", offers["availability"])
	}
}

func TestJSONLDProductStep_NoVariants(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget", Slug: "widget"}
	s := composition.NewJSONLDProductStep(
		&mockVariantRepo{variants: []catalog.Variant{}},
		&mockPriceRepo{},
		&mockStockRepo{},
	)
	ctx := composition.NewProductContext(&prod)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ld := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	if ld["offers"] != nil {
		t.Error("expected no offers when no variants")
	}
}

func TestJSONLDProductStep_DefaultCurrency(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget", Slug: "widget"}
	v, _ := catalog.NewVariant("v1", "p1", "SKU-1")
	amount := shared.MustNewMoney(1000, "EUR")
	price := pricing.Price{ID: "pr1", VariantID: "v1", Amount: amount}

	s := composition.NewJSONLDProductStep(
		&mockVariantRepo{variants: []catalog.Variant{v}},
		&mockPriceRepo{price: &price},
		&mockStockRepo{err: errors.New("no stock")},
	)
	ctx := composition.NewProductContext(&prod)
	// Currency left empty — should default to EUR.
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ld := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	offers := ld["offers"].(map[string]interface{})
	if offers["priceCurrency"] != "EUR" {
		t.Errorf("priceCurrency = %v, want EUR", offers["priceCurrency"])
	}
	if offers["price"] != "10.00" {
		t.Errorf("price = %v, want 10.00", offers["price"])
	}
	if _, ok := offers["availability"]; ok {
		t.Error("expected no availability when stock lookup fails")
	}
}
