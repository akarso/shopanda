package composition_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/testutil"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
)

// --- mocks ---

type ctxKey struct{}

type mockVariantRepo struct {
	variants    []catalog.Variant
	err         error
	capturedCtx context.Context
}

func (m *mockVariantRepo) FindByID(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) FindBySKUs(_ context.Context, skus []string) (map[string]*catalog.Variant, error) {
	out := make(map[string]*catalog.Variant, len(skus))
	for _, sku := range skus {
		for i := range m.variants {
			if m.variants[i].SKU == sku {
				v := m.variants[i]
				out[sku] = &v
				break
			}
		}
	}
	return out, nil
}
func (m *mockVariantRepo) ListByProductID(ctx context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	m.capturedCtx = ctx
	return m.variants, m.err
}
func (m *mockVariantRepo) ListByProductIDs(ctx context.Context, productIDs []string, limitPerProduct int) (map[string][]catalog.Variant, error) {
	m.capturedCtx = ctx
	if m.err != nil {
		return nil, m.err
	}
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

func (m *mockVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *mockVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }

type mockPriceRepo struct {
	price            *pricing.Price
	err              error
	capturedCtx      context.Context
	capturedVariant  string
	capturedCurrency string
	capturedStore    string
}

func (m *mockPriceRepo) FindByVariantCurrencyAndStore(ctx context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	m.capturedCtx = ctx
	m.capturedVariant = variantID
	m.capturedCurrency = currency
	m.capturedStore = storeID
	return m.price, m.err
}
func (m *mockPriceRepo) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*pricing.Price, error) {
	return testutil.FindByVariantsCurrencyAndStoreFromFind(ctx, m.FindByVariantCurrencyAndStore, variantIDs, currency, storeID)
}

func (m *mockPriceRepo) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}
func (m *mockPriceRepo) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}
func (m *mockPriceRepo) Upsert(_ context.Context, _ *pricing.Price) error { return nil }

type mockStockRepo struct {
	entry       inventory.StockEntry
	byVariant   map[string]inventory.StockEntry
	err         error
	capturedCtx context.Context
}

func (m *mockStockRepo) GetStock(ctx context.Context, variantID string) (inventory.StockEntry, error) {
	m.capturedCtx = ctx
	if m.byVariant != nil {
		if entry, ok := m.byVariant[variantID]; ok {
			return entry, m.err
		}
		return inventory.StockEntry{}, m.err
	}
	return m.entry, m.err
}
func (m *mockStockRepo) SetStock(_ context.Context, _ *inventory.StockEntry) error   { return nil }
func (m *mockStockRepo) SetStocks(_ context.Context, _ []inventory.StockEntry) error { return nil }
func (m *mockStockRepo) ListStock(_ context.Context, _, _ int) ([]inventory.StockEntry, error) {
	return nil, nil
}
func (m *mockStockRepo) ListInventory(_ context.Context, _, _ int, _ string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}
func (m *mockStockRepo) GetInventoryItem(_ context.Context, _ string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}

// --- tests ---

func TestJSONLDProductStep_Name(t *testing.T) {
	s := composition.NewJSONLDProductStep()
	if s.Name() != "seo_jsonld" {
		t.Errorf("Name() = %q, want seo_jsonld", s.Name())
	}
}

func TestJSONLDProductStep_NilProduct(t *testing.T) {
	s := composition.NewJSONLDProductStep()
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
	s := composition.NewJSONLDProductStep()
	ctx := composition.NewProductContext(&prod)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(ctx.Blocks))
	}
	ld := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	if ld["name"] != "Widget" || ld["url"] != "/products/widget" {
		t.Fatalf("ld = %#v", ld)
	}
	if ld["offers"] != nil {
		t.Fatal("expected no offers without meta.offer")
	}
}

func TestJSONLDProductStep_OfferFromMeta(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget", Slug: "widget"}
	s := composition.NewJSONLDProductStep()
	amount := int64(2999)
	ctx := composition.NewProductContext(&prod)
	ctx.Meta[composition.MetaOffer] = composition.PublicOffer{
		VariantID: "v-cheap", Amount: &amount, Currency: "USD", InStock: true,
	}
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	offers := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})["offers"].(map[string]interface{})
	if offers["price"] != "29.99" || offers["priceCurrency"] != "USD" || offers["availability"] != "https://schema.org/InStock" {
		t.Fatalf("offers = %#v", offers)
	}

	amount = 1000
	ctx = composition.NewProductContext(&prod)
	ctx.Meta[composition.MetaOffer] = composition.PublicOffer{
		VariantID: "v-cheap", Amount: &amount, Currency: "EUR", InStock: false,
	}
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply out of stock: %v", err)
	}
	offers = ctx.Blocks[0].Data["jsonld"].(map[string]interface{})["offers"].(map[string]interface{})
	if offers["price"] != "10.00" || offers["availability"] != "https://schema.org/OutOfStock" {
		t.Fatalf("out of stock offers = %#v", offers)
	}
}

func TestJSONLDProductStep_OmitsStockOnlyAndGrouped(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget", Slug: "widget"}
	s := composition.NewJSONLDProductStep()
	ctx := composition.NewProductContext(&prod)
	ctx.Meta[composition.MetaOffer] = composition.PublicOffer{InStock: true}
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ld := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	if ld["offers"] != nil {
		t.Fatalf("stock-only offer = %#v", ld["offers"])
	}

	ctx = composition.NewProductContext(&prod)
	ctx.Meta[composition.MetaOffer] = composition.PublicOffer{}
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply grouped: %v", err)
	}
	ld = ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	if ld["offers"] != nil {
		t.Fatalf("grouped offer = %#v", ld["offers"])
	}
}
