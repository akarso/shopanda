package pricing_test

import (
	"context"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type rollupVariants struct {
	variants map[string]catalog.Variant
}

func (r rollupVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	v, ok := r.variants[id]
	if !ok {
		return nil, nil
	}
	cp := v
	return &cp, nil
}
func (rollupVariants) FindBySKU(context.Context, string) (*catalog.Variant, error) { return nil, nil }
func (rollupVariants) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (r rollupVariants) ListByProductID(_ context.Context, productID string, _, limit int) ([]catalog.Variant, error) {
	var out []catalog.Variant
	for _, v := range r.variants {
		if v.ProductID == productID {
			out = append(out, v)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r rollupVariants) ListByProductIDs(_ context.Context, productIDs []string, limit int) (map[string][]catalog.Variant, error) {
	want := make(map[string]struct{}, len(productIDs))
	for _, id := range productIDs {
		want[id] = struct{}{}
	}
	out := make(map[string][]catalog.Variant)
	for _, v := range r.variants {
		if _, ok := want[v.ProductID]; !ok {
			continue
		}
		out[v.ProductID] = append(out[v.ProductID], v)
	}
	if limit > 0 {
		for id, vs := range out {
			if len(vs) > limit {
				out[id] = vs[:limit]
			}
		}
	}
	return out, nil
}
func (rollupVariants) Create(context.Context, *catalog.Variant) error { return nil }
func (rollupVariants) Update(context.Context, *catalog.Variant) error { return nil }

type rollupProducts struct {
	products map[string]*catalog.Product
}

func (r rollupProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	p, ok := r.products[id]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}
func (rollupProducts) FindBySlug(context.Context, string) (*catalog.Product, error) { return nil, nil }
func (rollupProducts) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (rollupProducts) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (rollupProducts) Create(context.Context, *catalog.Product) error { return nil }
func (rollupProducts) Update(context.Context, *catalog.Product) error { return nil }

type rollupComponents struct {
	rows []catalog.BundleComponent
}

func (r rollupComponents) ListByBundleID(_ context.Context, bundleProductID string) ([]catalog.BundleComponent, error) {
	out := make([]catalog.BundleComponent, 0)
	for _, row := range r.rows {
		if row.BundleProductID == bundleProductID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (rollupComponents) CountByBundleID(context.Context, string) (int, error)    { return 0, nil }
func (rollupComponents) CountByComponentID(context.Context, string) (int, error) { return 0, nil }
func (rollupComponents) Upsert(context.Context, *catalog.BundleComponent) error  { return nil }
func (rollupComponents) Remove(context.Context, string, string) error            { return nil }

func activeProduct(id string, typ catalog.Type, mode catalog.PricingMode) *catalog.Product {
	return &catalog.Product{ID: id, Status: catalog.StatusActive, Type: typ, PricingMode: mode}
}

func sources(prices *mockPriceRepo, variants map[string]catalog.Variant, products map[string]*catalog.Product, components []catalog.BundleComponent) pricing.UnitPriceSources {
	return pricing.UnitPriceSources{
		Prices:     prices,
		Variants:   rollupVariants{variants: variants},
		Products:   rollupProducts{products: products},
		Components: rollupComponents{rows: components},
	}
}

func TestResolveUnitPrice_FixedIgnoresComponentPriceChange(t *testing.T) {
	prices := makeMockRepo(
		mockEntry{"bundle-v", "EUR", 1000, ""},
		mockEntry{"comp-v", "EUR", 100, ""},
	)
	src := sources(prices,
		map[string]catalog.Variant{
			"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
			"comp-v":   {ID: "comp-v", ProductID: "comp"},
		},
		map[string]*catalog.Product{
			"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeFixed),
			"comp":   activeProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed),
		},
		[]catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 1, Required: true,
		}},
	)

	got, err := pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Amount() != 1000 {
		t.Fatalf("amount = %d, want 1000", got.Amount())
	}

	prices.prices["comp-v"][priceKey{"EUR", ""}].Amount = shared.MustNewMoney(900, "EUR")
	got, err = pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
	if err != nil {
		t.Fatalf("resolve after component change: %v", err)
	}
	if got.Amount() != 1000 {
		t.Fatalf("amount = %d, want 1000 (fixed ignores components)", got.Amount())
	}
	if prices.upserts != 0 {
		t.Fatalf("upserts = %d, want 0", prices.upserts)
	}
}

func TestResolveUnitPrice_DynamicFollowsComponentPriceWithoutWrite(t *testing.T) {
	prices := makeMockRepo(
		mockEntry{"comp-a", "EUR", 100, ""},
		mockEntry{"comp-b", "EUR", 50, ""},
		mockEntry{"opt-v", "EUR", 999, ""},
	)
	src := sources(prices,
		map[string]catalog.Variant{
			"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
			"comp-a":   {ID: "comp-a", ProductID: "a"},
			"comp-b":   {ID: "comp-b", ProductID: "b"},
			"opt-v":    {ID: "opt-v", ProductID: "opt"},
		},
		map[string]*catalog.Product{
			"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
			"a":      activeProduct("a", catalog.TypeSimple, catalog.PricingModeFixed),
			"b":      activeProduct("b", catalog.TypeSimple, catalog.PricingModeFixed),
			"opt":    activeProduct("opt", catalog.TypeSimple, catalog.PricingModeFixed),
		},
		[]catalog.BundleComponent{
			{BundleProductID: "bundle", ComponentProductID: "a", Quantity: 2, Required: true},
			{BundleProductID: "bundle", ComponentProductID: "b", Quantity: 1, Required: true},
			{BundleProductID: "bundle", ComponentProductID: "opt", Quantity: 1, Required: false},
		},
	)

	got, err := pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Amount() != 250 {
		t.Fatalf("amount = %d, want 250 (2*100 + 50, optional excluded)", got.Amount())
	}

	prices.prices["comp-a"][priceKey{"EUR", ""}].Amount = shared.MustNewMoney(80, "EUR")
	got, err = pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
	if err != nil {
		t.Fatalf("resolve after change: %v", err)
	}
	if got.Amount() != 210 {
		t.Fatalf("amount = %d, want 210", got.Amount())
	}
	if _, ok := prices.prices["bundle-v"]; ok {
		t.Fatal("dynamic resolution wrote a bundle price row")
	}
	if prices.upserts != 0 {
		t.Fatalf("upserts = %d, want 0", prices.upserts)
	}

	step := pricing.NewBasePriceStep(src)
	if step.Name() != "base" {
		t.Fatalf("step name = %q, want base", step.Name())
	}
	pctx, err := domain.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	item, err := domain.NewPricingItem("bundle-v", 3, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("item: %v", err)
	}
	pctx.Items = []domain.PricingItem{item}
	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if pctx.Items[0].UnitPrice.Amount() != 210 || pctx.Items[0].Total.Amount() != 630 {
		t.Fatalf("pipeline unit=%d total=%d, want 210/630", pctx.Items[0].UnitPrice.Amount(), pctx.Items[0].Total.Amount())
	}
}

func TestResolveUnitPrice_DynamicStoreFallsBackToGlobalComponentPrice(t *testing.T) {
	prices := makeMockRepo(mockEntry{"comp-v", "EUR", 70, ""})
	src := sources(prices,
		map[string]catalog.Variant{
			"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
			"comp-v":   {ID: "comp-v", ProductID: "comp"},
		},
		map[string]*catalog.Product{
			"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
			"comp":   activeProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed),
		},
		[]catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 1, Required: true,
		}},
	)
	got, err := pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "store-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Amount() != 70 || got.Currency() != "EUR" {
		t.Fatalf("amount = %s, want 70 EUR from the global component row", got)
	}
}

func TestResolveUnitPrice_DynamicSumZero(t *testing.T) {
	prices := makeMockRepo(mockEntry{"comp-v", "EUR", 0, ""})
	src := sources(prices,
		map[string]catalog.Variant{
			"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
			"comp-v":   {ID: "comp-v", ProductID: "comp"},
		},
		map[string]*catalog.Product{
			"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
			"comp":   activeProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed),
		},
		[]catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 2, Required: true,
		}},
	)
	got, err := pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Amount() != 0 || got.Currency() != "EUR" {
		t.Fatalf("amount = %s, want 0 EUR", got)
	}
}

func TestResolveUnitPrice_GroupedRejectsStalePrice(t *testing.T) {
	prices := makeMockRepo(mockEntry{"group-v", "EUR", 500, ""})
	src := sources(prices,
		map[string]catalog.Variant{"group-v": {ID: "group-v", ProductID: "group"}},
		map[string]*catalog.Product{"group": activeProduct("group", catalog.TypeGrouped, catalog.PricingModeFixed)},
		nil,
	)
	_, err := pricing.ResolveUnitPrice(context.Background(), src, "group-v", "EUR", "")
	if err == nil || !strings.Contains(err.Error(), "grouped") {
		t.Fatalf("err = %v, want grouped validation", err)
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestResolveUnitPrice_DynamicMultiVariantComponent(t *testing.T) {
	prices := makeMockRepo(
		mockEntry{"v1", "EUR", 10, ""},
		mockEntry{"v2", "EUR", 20, ""},
	)
	src := sources(prices,
		map[string]catalog.Variant{
			"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
			"v1":       {ID: "v1", ProductID: "comp"},
			"v2":       {ID: "v2", ProductID: "comp"},
		},
		map[string]*catalog.Product{
			"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
			"comp":   activeProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed),
		},
		[]catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 1, Required: true,
		}},
	)
	_, err := pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
	if err == nil || !strings.Contains(err.Error(), "exactly one variant") {
		t.Fatalf("err = %v, want exactly one variant", err)
	}
}

func TestResolveUnitPrice_DynamicEmptyRequiredSet(t *testing.T) {
	prices := makeMockRepo(mockEntry{"opt-v", "EUR", 10, ""})
	src := sources(prices,
		map[string]catalog.Variant{
			"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
			"opt-v":    {ID: "opt-v", ProductID: "opt"},
		},
		map[string]*catalog.Product{
			"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
			"opt":    activeProduct("opt", catalog.TypeSimple, catalog.PricingModeFixed),
		},
		[]catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "opt", Quantity: 1, Required: false,
		}},
	)
	_, err := pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
	if err == nil || !strings.Contains(err.Error(), "no required components") {
		t.Fatalf("err = %v, want no required components", err)
	}
}

func TestResolveUnitPrice_NilCatalogDoesNotReturnStoredPrice(t *testing.T) {
	prices := makeMockRepo(mockEntry{"v", "EUR", 500, ""})
	_, err := pricing.ResolveUnitPrice(context.Background(), pricing.UnitPriceSources{Prices: prices}, "v", "EUR", "")
	if err == nil {
		t.Fatal("nil catalog returned a price")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want validation", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("NewBasePriceStep without catalog did not panic")
		}
	}()
	pricing.NewBasePriceStep(pricing.UnitPriceSources{Prices: prices})
}

func TestResolveUnitPrice_DynamicIgnoresOwnStoredRow(t *testing.T) {
	prices := makeMockRepo(
		mockEntry{"bundle-v", "EUR", 9999, ""},
		mockEntry{"comp-v", "EUR", 40, ""},
	)
	src := sources(prices,
		map[string]catalog.Variant{
			"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
			"comp-v":   {ID: "comp-v", ProductID: "comp"},
		},
		map[string]*catalog.Product{
			"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
			"comp":   activeProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed),
		},
		[]catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 2, Required: true,
		}},
	)
	got, err := pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Amount() != 80 {
		t.Fatalf("amount = %d, want 80 (own row ignored)", got.Amount())
	}
}

func TestResolveUnitPrice_InactiveAndNonLeafComponents(t *testing.T) {
	prices := makeMockRepo(mockEntry{"comp-v", "EUR", 10, ""})
	variants := map[string]catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
		"comp-v":   {ID: "comp-v", ProductID: "comp"},
	}
	components := []catalog.BundleComponent{{
		BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 1, Required: true,
	}}
	for _, status := range []catalog.Status{catalog.StatusDraft, catalog.StatusArchived} {
		comp := activeProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed)
		comp.Status = status
		src := sources(prices, variants, map[string]*catalog.Product{
			"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
			"comp":   comp,
		}, components)
		_, err := pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
		if err == nil || !strings.Contains(err.Error(), "not active") {
			t.Fatalf("status %s err = %v, want not active", status, err)
		}
		if !apperror.Is(err, apperror.CodeValidation) {
			t.Fatalf("status %s err = %v, want validation", status, err)
		}
	}

	comp := activeProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed)
	comp.Status = catalog.StatusDraft
	src := sources(prices, variants, map[string]*catalog.Product{
		"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
		"comp":   comp,
	}, components)
	got, err := pricing.ResolveUnitPrice(catalog.WithOperatorProductReadScope(context.Background()), src, "bundle-v", "EUR", "")
	if err != nil {
		t.Fatalf("operator scope: %v", err)
	}
	if got.Amount() != 10 {
		t.Fatalf("operator amount = %d, want 10", got.Amount())
	}

	src = sources(prices, variants, map[string]*catalog.Product{
		"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
		"comp":   activeProduct("comp", catalog.TypeGrouped, catalog.PricingModeFixed),
	}, components)
	_, err = pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
	if err == nil || !strings.Contains(err.Error(), "simple, virtual, or downloadable") {
		t.Fatalf("non-leaf err = %v", err)
	}
}

func TestResolveUnitPrice_RequiredComponentCap(t *testing.T) {
	components := make([]catalog.BundleComponent, 51)
	for i := range components {
		components[i] = catalog.BundleComponent{
			BundleProductID: "bundle", ComponentProductID: "c", Quantity: 1, Required: true,
		}
	}
	src := sources(makeMockRepo(), map[string]catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
	}, map[string]*catalog.Product{
		"bundle": activeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
	}, components)
	_, err := pricing.ResolveUnitPrice(context.Background(), src, "bundle-v", "EUR", "")
	if err == nil || !strings.Contains(err.Error(), "maximum is 50") {
		t.Fatalf("err = %v, want cap", err)
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestResolveUnitPrice_MissingVariantIsNotFound(t *testing.T) {
	src := sources(makeMockRepo(), map[string]catalog.Variant{}, map[string]*catalog.Product{}, nil)
	_, err := pricing.ResolveUnitPrice(context.Background(), src, "missing", "EUR", "")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
}

type countingVariants struct {
	rollupVariants
	finds int
}

func (c *countingVariants) FindByID(ctx context.Context, id string) (*catalog.Variant, error) {
	c.finds++
	return c.rollupVariants.FindByID(ctx, id)
}

func TestResolveUnitPrice_CachesForTheRequest(t *testing.T) {
	prices := makeMockRepo(mockEntry{"v", "EUR", 15, ""})
	variants := &countingVariants{rollupVariants: rollupVariants{variants: map[string]catalog.Variant{
		"v": {ID: "v", ProductID: "p"},
	}}}
	src := pricing.UnitPriceSources{
		Prices:     prices,
		Variants:   variants,
		Products:   rollupProducts{products: map[string]*catalog.Product{"p": activeProduct("p", catalog.TypeSimple, catalog.PricingModeFixed)}},
		Components: rollupComponents{},
	}
	ctx := pricing.WithUnitPriceCache(context.Background())
	first, err := pricing.ResolveUnitPrice(ctx, src, "v", "EUR", "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := pricing.ResolveUnitPrice(ctx, src, "v", "EUR", "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.Amount() != 15 || second.Amount() != 15 {
		t.Fatalf("amounts = %d/%d, want 15/15", first.Amount(), second.Amount())
	}
	if variants.finds != 1 {
		t.Fatalf("variant reads = %d, want 1", variants.finds)
	}
}
