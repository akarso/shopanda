package composition_test

import (
	"context"

	"errors"
	"github.com/akarso/shopanda/internal/testutil"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
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

func TestJSONLDProductStep_NilCtx(t *testing.T) {
	s := composition.NewJSONLDProductStep(&mockVariantRepo{}, &mockPriceRepo{}, &mockStockRepo{})
	if err := s.Apply(nil); err != nil {
		t.Fatalf("Apply(nil): %v", err)
	}
}

func TestJSONLDProductStep_ForwardsContext(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget", Slug: "widget"}
	v, _ := catalog.NewVariant("v1", "p1", "SKU-1")
	amount := shared.MustNewMoney(500, "EUR")
	price := pricing.Price{ID: "pr1", VariantID: "v1", Amount: amount}
	stock, _ := inventory.NewStockEntry("v1", 1)

	vr := &mockVariantRepo{variants: []catalog.Variant{v}}
	pr := &mockPriceRepo{price: &price}
	sr := &mockStockRepo{entry: stock}

	s := composition.NewJSONLDProductStep(vr, pr, sr)
	customCtx := context.WithValue(context.Background(), ctxKey{}, "test-value")
	ctx := composition.NewProductContext(&prod)
	ctx.Ctx = customCtx

	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if vr.capturedCtx != customCtx {
		t.Error("variant repo did not receive forwarded context")
	}
	if pr.capturedCtx != customCtx {
		t.Error("price repo did not receive forwarded context")
	}
	if sr.capturedCtx != customCtx {
		t.Error("stock repo did not receive forwarded context")
	}
}

func TestJSONLDProductStep_NoOffer_WhenAllLookupsFail(t *testing.T) {
	prod := catalog.Product{ID: "p1", Name: "Widget", Slug: "widget"}
	v, _ := catalog.NewVariant("v1", "p1", "SKU-1")

	s := composition.NewJSONLDProductStep(
		&mockVariantRepo{variants: []catalog.Variant{v}},
		&mockPriceRepo{err: errors.New("no price")},
		&mockStockRepo{err: errors.New("no stock")},
	)
	ctx := composition.NewProductContext(&prod)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ld := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	if ld["offers"] != nil {
		t.Error("expected no offers when both price and stock lookups fail")
	}
}

func TestJSONLDProductStep_DynamicSkipsStoredPrice(t *testing.T) {
	prod := catalog.Product{
		ID: "p1", Name: "Kit", Slug: "kit",
		Type: catalog.TypeBundle, PricingMode: catalog.PricingModeDynamic,
	}
	v, err := catalog.NewVariant("v1", "p1", "SKU-1")
	if err != nil {
		t.Fatalf("variant: %v", err)
	}
	stock, err := inventory.NewStockEntry("v1", 3)
	if err != nil {
		t.Fatalf("stock: %v", err)
	}
	s := composition.NewJSONLDProductStep(
		&mockVariantRepo{variants: []catalog.Variant{v}},
		&mockPriceRepo{price: &pricing.Price{ID: "pr", VariantID: "v1", Amount: shared.MustNewMoney(1000, "EUR")}},
		&mockStockRepo{entry: stock},
	)
	ctx := composition.NewProductContext(&prod)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ld := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	if ld["offers"] != nil {
		t.Fatal("dynamic bundle emitted an offer before ResolveUnitPrice")
	}
}

type jsonLDProducts struct {
	products map[string]*catalog.Product
}

func (m jsonLDProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return m.products[id], nil
}
func (jsonLDProducts) FindBySlug(context.Context, string) (*catalog.Product, error) { return nil, nil }
func (jsonLDProducts) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (jsonLDProducts) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (jsonLDProducts) Create(context.Context, *catalog.Product) error { return nil }
func (jsonLDProducts) Update(context.Context, *catalog.Product) error { return nil }

type jsonLDComponents struct {
	rows []catalog.BundleComponent
}

func (m jsonLDComponents) ListByBundleID(_ context.Context, bundleProductID string) ([]catalog.BundleComponent, error) {
	out := make([]catalog.BundleComponent, 0)
	for _, row := range m.rows {
		if row.BundleProductID == bundleProductID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (jsonLDComponents) CountByBundleID(context.Context, string) (int, error) { return 0, nil }
func (jsonLDComponents) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (jsonLDComponents) Upsert(context.Context, *catalog.BundleComponent) error { return nil }
func (jsonLDComponents) Remove(context.Context, string, string) error           { return nil }

func TestJSONLDProductStep_DynamicStockInStockFromRollup(t *testing.T) {
	bundleVar, err := catalog.NewVariant("bundle-v", "bundle", "SKU-B")
	if err != nil {
		t.Fatal(err)
	}
	compVar, err := catalog.NewVariant("comp-v", "comp", "SKU-C")
	if err != nil {
		t.Fatal(err)
	}
	own, err := inventory.NewStockEntry("bundle-v", 0)
	if err != nil {
		t.Fatal(err)
	}
	compStock, err := inventory.NewStockEntry("comp-v", 4)
	if err != nil {
		t.Fatal(err)
	}
	products := jsonLDProducts{products: map[string]*catalog.Product{
		"bundle": {ID: "bundle", Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
		"comp":   {ID: "comp", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	components := jsonLDComponents{rows: []catalog.BundleComponent{
		{BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 2, Required: true},
		{BundleProductID: "bundle", ComponentProductID: "opt", Quantity: 9, Required: false},
	}}
	s := composition.NewJSONLDProductStep(
		&mockVariantRepo{variants: []catalog.Variant{bundleVar, compVar}},
		&mockPriceRepo{price: &pricing.Price{ID: "pr", VariantID: "bundle-v", Amount: shared.MustNewMoney(2500, "EUR")}},
		&mockStockRepo{byVariant: map[string]inventory.StockEntry{"bundle-v": own, "comp-v": compStock}},
	).WithBundleStockRollup(products, components)

	prod := catalog.Product{
		ID: "bundle", Name: "Kit", Slug: "kit",
		Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeDynamic,
	}
	ctx := composition.NewProductContext(&prod)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ld := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	offers, ok := ld["offers"].(map[string]interface{})
	if !ok {
		t.Fatal("expected an offer for a fixed-price dynamic-stock bundle")
	}
	if offers["availability"] != "https://schema.org/InStock" {
		t.Fatalf("availability = %v, want InStock from component rollup (bundle stock is 0)", offers["availability"])
	}

	short, err := inventory.NewStockEntry("comp-v", 1)
	if err != nil {
		t.Fatal(err)
	}
	s = composition.NewJSONLDProductStep(
		&mockVariantRepo{variants: []catalog.Variant{bundleVar, compVar}},
		&mockPriceRepo{price: &pricing.Price{ID: "pr", VariantID: "bundle-v", Amount: shared.MustNewMoney(2500, "EUR")}},
		&mockStockRepo{byVariant: map[string]inventory.StockEntry{"bundle-v": own, "comp-v": short}},
	).WithBundleStockRollup(products, components)
	ctx = composition.NewProductContext(&prod)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply short: %v", err)
	}
	offers = ctx.Blocks[0].Data["jsonld"].(map[string]interface{})["offers"].(map[string]interface{})
	if offers["availability"] != "https://schema.org/OutOfStock" {
		t.Fatalf("availability = %v, want OutOfStock when floor(stock/quantity) is 0", offers["availability"])
	}

	pricedDynamic := prod
	pricedDynamic.PricingMode = catalog.PricingModeDynamic
	s = composition.NewJSONLDProductStep(
		&mockVariantRepo{variants: []catalog.Variant{bundleVar, compVar}},
		&mockPriceRepo{price: &pricing.Price{ID: "pr", VariantID: "bundle-v", Amount: shared.MustNewMoney(2500, "EUR")}},
		&mockStockRepo{byVariant: map[string]inventory.StockEntry{"bundle-v": own, "comp-v": compStock}},
	).WithBundleStockRollup(products, components)
	ctx = composition.NewProductContext(&pricedDynamic)
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply dynamic price: %v", err)
	}
	ld = ctx.Blocks[0].Data["jsonld"].(map[string]interface{})
	if ld["offers"] != nil {
		t.Fatal("dynamic pricing still emitted an offer")
	}
}

func TestJSONLDProductStep_DynamicStockIgnoresStuffedMeta(t *testing.T) {
	bundleVar, err := catalog.NewVariant("bundle-v", "bundle", "SKU-B")
	if err != nil {
		t.Fatal(err)
	}
	compVar, err := catalog.NewVariant("comp-v", "comp", "SKU-C")
	if err != nil {
		t.Fatal(err)
	}
	own, err := inventory.NewStockEntry("bundle-v", 0)
	if err != nil {
		t.Fatal(err)
	}
	compStock, err := inventory.NewStockEntry("comp-v", 4)
	if err != nil {
		t.Fatal(err)
	}
	products := jsonLDProducts{products: map[string]*catalog.Product{
		"bundle": {ID: "bundle", Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
		"comp":   {ID: "comp", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	components := jsonLDComponents{rows: []catalog.BundleComponent{
		{BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 2, Required: true},
	}}
	s := composition.NewJSONLDProductStep(
		&mockVariantRepo{variants: []catalog.Variant{bundleVar, compVar}},
		&mockPriceRepo{price: &pricing.Price{ID: "pr", VariantID: "bundle-v", Amount: shared.MustNewMoney(2500, "EUR")}},
		&mockStockRepo{byVariant: map[string]inventory.StockEntry{"bundle-v": own, "comp-v": compStock}},
	).WithBundleStockRollup(products, components)

	prod := catalog.Product{
		ID: "bundle", Name: "Kit", Slug: "kit",
		Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeDynamic,
	}
	ctx := composition.NewProductContext(&prod)
	ctx.Meta["quantity_positive"] = false
	ctx.Meta["visibility_inputs"] = catalog.VisibilityInputs{QuantityPositive: false}
	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	offers := ctx.Blocks[0].Data["jsonld"].(map[string]interface{})["offers"].(map[string]interface{})
	if offers["availability"] != "https://schema.org/InStock" {
		t.Fatalf("availability = %v, want InStock from ResolveAvailableQty; stuffed Meta is not a fact source", offers["availability"])
	}
}
