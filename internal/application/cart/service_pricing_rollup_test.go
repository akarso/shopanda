package cart_test

import (
	"context"
	"strings"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type cartRollupVariants struct {
	variants map[string]catalog.Variant
}

func (r cartRollupVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	v, ok := r.variants[id]
	if !ok {
		return nil, nil
	}
	cp := v
	return &cp, nil
}
func (cartRollupVariants) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (cartRollupVariants) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (r cartRollupVariants) ListByProductID(_ context.Context, productID string, _, limit int) ([]catalog.Variant, error) {
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
func (r cartRollupVariants) ListByProductIDs(_ context.Context, productIDs []string, limit int) (map[string][]catalog.Variant, error) {
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
func (cartRollupVariants) Create(context.Context, *catalog.Variant) error { return nil }
func (cartRollupVariants) Update(context.Context, *catalog.Variant) error { return nil }

type cartRollupProducts struct {
	products      map[string]*catalog.Product
	hideNonActive bool
}

func (r cartRollupProducts) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	p, ok := r.products[id]
	if !ok {
		return nil, nil
	}
	if r.hideNonActive && p.Status != catalog.StatusActive && !catalog.IncludeNonActiveProducts(ctx) {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}
func (cartRollupProducts) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (cartRollupProducts) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (cartRollupProducts) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (cartRollupProducts) Create(context.Context, *catalog.Product) error { return nil }
func (cartRollupProducts) Update(context.Context, *catalog.Product) error { return nil }

type cartRollupComponents struct {
	rows []catalog.BundleComponent
}

func (r cartRollupComponents) ListByBundleID(_ context.Context, bundleProductID string) ([]catalog.BundleComponent, error) {
	out := make([]catalog.BundleComponent, 0)
	for _, row := range r.rows {
		if row.BundleProductID == bundleProductID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (cartRollupComponents) CountByBundleID(context.Context, string) (int, error) { return 0, nil }
func (cartRollupComponents) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (cartRollupComponents) Upsert(context.Context, *catalog.BundleComponent) error { return nil }
func (cartRollupComponents) Remove(context.Context, string, string) error           { return nil }

func rollupProduct(id string, typ catalog.Type, mode catalog.PricingMode) *catalog.Product {
	return &catalog.Product{ID: id, Status: catalog.StatusActive, Type: typ, PricingMode: mode}
}

func newRollupService(prices *stubPriceRepo, variants map[string]catalog.Variant, products map[string]*catalog.Product, components []catalog.BundleComponent) *cartApp.Service {
	vr := cartRollupVariants{variants: variants}
	pr := cartRollupProducts{products: products}
	cr := cartRollupComponents{rows: components}
	src := appPricing.UnitPriceSources{Prices: prices, Variants: vr, Products: pr, Components: cr}
	pipeline := pricing.NewPipeline(
		appPricing.NewBasePriceStep(src),
		pricing.NewFinalizeStep(),
	)
	facts := purchasableFacts{}
	for id := range products {
		facts[id] = sellableSnap(catalog.DefaultVisibilityAxes())
	}
	return cartApp.NewService(newStubCartRepo(), prices, nil, nil, vr, pr, cr, pipeline, testLogger(), testBus(), nil, nil).
		WithVisibilityFacts(facts, catalog.VisibilityOptions{})
}

func TestService_AddItem_DynamicBundleWithoutOwnPrice(t *testing.T) {
	prices := newStubPriceRepo()
	prices.set("comp-v", "EUR", 0)
	variants := map[string]catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
		"comp-v":   {ID: "comp-v", ProductID: "comp"},
	}
	products := map[string]*catalog.Product{
		"bundle": rollupProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
		"comp":   rollupProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed),
	}
	components := []catalog.BundleComponent{{
		BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 1, Required: true,
	}}
	svc := newRollupService(prices, variants, products, components)

	cart, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	got, err := svc.AddItem(context.Background(), cart.ID, "", "bundle-v", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem sum 0: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].UnitPrice.Amount() != 0 {
		t.Fatalf("item = %+v, want unit price 0", got.Items)
	}
	if prices.writes != 0 {
		t.Fatalf("price writes = %d, want 0", prices.writes)
	}
}

func TestService_AddItem_DynamicReflectsComponentChange(t *testing.T) {
	prices := newStubPriceRepo()
	prices.set("comp-a", "EUR", 100)
	prices.set("comp-b", "EUR", 40)
	variants := map[string]catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
		"comp-a":   {ID: "comp-a", ProductID: "a"},
		"comp-b":   {ID: "comp-b", ProductID: "b"},
	}
	products := map[string]*catalog.Product{
		"bundle": rollupProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
		"a":      rollupProduct("a", catalog.TypeSimple, catalog.PricingModeFixed),
		"b":      rollupProduct("b", catalog.TypeSimple, catalog.PricingModeFixed),
	}
	components := []catalog.BundleComponent{
		{BundleProductID: "bundle", ComponentProductID: "a", Quantity: 2, Required: true},
		{BundleProductID: "bundle", ComponentProductID: "b", Quantity: 1, Required: true},
	}
	svc := newRollupService(prices, variants, products, components)
	cart, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	got, err := svc.AddItem(context.Background(), cart.ID, "", "bundle-v", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if got.Items[0].UnitPrice.Amount() != 240 {
		t.Fatalf("unit = %d, want 240", got.Items[0].UnitPrice.Amount())
	}

	prices.set("comp-a", "EUR", 80)
	got, err = svc.AddItem(context.Background(), cart.ID, "", "bundle-v", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem after component change: %v", err)
	}
	if got.Items[0].UnitPrice.Amount() != 200 {
		t.Fatalf("unit = %d, want 200 after component price change", got.Items[0].UnitPrice.Amount())
	}
	if prices.writes != 0 {
		t.Fatalf("price writes = %d, want 0 (dynamic sum is not stored)", prices.writes)
	}
}

func TestService_AddItem_FixedIgnoresComponentPriceChange(t *testing.T) {
	prices := newStubPriceRepo()
	prices.set("bundle-v", "EUR", 1000)
	prices.set("comp-v", "EUR", 100)
	variants := map[string]catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
		"comp-v":   {ID: "comp-v", ProductID: "comp"},
	}
	products := map[string]*catalog.Product{
		"bundle": rollupProduct("bundle", catalog.TypeBundle, catalog.PricingModeFixed),
		"comp":   rollupProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed),
	}
	components := []catalog.BundleComponent{{
		BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 1, Required: true,
	}}
	svc := newRollupService(prices, variants, products, components)
	cart, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(context.Background(), cart.ID, "", "bundle-v", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	prices.set("comp-v", "EUR", 900)
	got, err := svc.AddItem(context.Background(), cart.ID, "", "bundle-v", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem after component change: %v", err)
	}
	if got.Items[0].UnitPrice.Amount() != 1000 {
		t.Fatalf("unit = %d, want 1000", got.Items[0].UnitPrice.Amount())
	}
}

func TestService_AddItem_GroupedRejectsStalePrice(t *testing.T) {
	prices := newStubPriceRepo()
	prices.set("group-v", "EUR", 500)
	variants := map[string]catalog.Variant{
		"group-v": {ID: "group-v", ProductID: "group"},
	}
	products := map[string]*catalog.Product{
		"group": rollupProduct("group", catalog.TypeGrouped, catalog.PricingModeFixed),
	}
	svc := newRollupService(prices, variants, products, nil)
	cart, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), cart.ID, "", "group-v", 1, cartApp.AddItemOptions{})
	if err == nil || !strings.Contains(err.Error(), "not purchasable") {
		t.Fatalf("err = %v, want not purchasable", err)
	}
	if strings.Contains(err.Error(), catalog.GroupedPriceRejectedMessage) {
		t.Fatalf("err = %v, want purchasability failure before price resolution", err)
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
	if prices.reads != 0 {
		t.Fatalf("price reads = %d, want 0", prices.reads)
	}
}

func TestService_AddItem_DynamicBundleWrongCurrency(t *testing.T) {
	prices := newStubPriceRepo()
	prices.set("comp-v", "EUR", 150)
	variants := map[string]catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
		"comp-v":   {ID: "comp-v", ProductID: "comp"},
	}
	products := map[string]*catalog.Product{
		"bundle": rollupProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
		"comp":   rollupProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed),
	}
	components := []catalog.BundleComponent{{
		BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 1, Required: true,
	}}
	svc := newRollupService(prices, variants, products, components)
	cart, err := svc.CreateCart(context.Background(), "", "USD")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), cart.ID, "", "bundle-v", 1, cartApp.AddItemOptions{})
	if err == nil {
		t.Fatal("AddItem succeeded, want validation")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
	if !strings.Contains(err.Error(), "USD") {
		t.Fatalf("err = %v, want the cart currency in the validation error", err)
	}
}

func TestService_AddItem_DynamicMultiVariantComponent(t *testing.T) {
	prices := newStubPriceRepo()
	prices.set("v1", "EUR", 10)
	prices.set("v2", "EUR", 20)
	variants := map[string]catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
		"v1":       {ID: "v1", ProductID: "comp"},
		"v2":       {ID: "v2", ProductID: "comp"},
	}
	products := map[string]*catalog.Product{
		"bundle": rollupProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
		"comp":   rollupProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed),
	}
	components := []catalog.BundleComponent{{
		BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 1, Required: true,
	}}
	svc := newRollupService(prices, variants, products, components)
	cart, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	_, err = svc.AddItem(context.Background(), cart.ID, "", "bundle-v", 1, cartApp.AddItemOptions{})
	if err == nil || !strings.Contains(err.Error(), "exactly one variant") {
		t.Fatalf("err = %v, want exactly one variant", err)
	}
}

func TestService_Recalculate_ArchivedLine(t *testing.T) {
	prices := newStubPriceRepo()
	prices.set("arch-v", "EUR", 1500)
	prices.set("live-v", "EUR", 800)
	variants := map[string]catalog.Variant{
		"arch-v": {ID: "arch-v", ProductID: "arch"},
		"live-v": {ID: "live-v", ProductID: "live"},
	}
	products := map[string]*catalog.Product{
		"arch": rollupProduct("arch", catalog.TypeSimple, catalog.PricingModeFixed),
		"live": rollupProduct("live", catalog.TypeSimple, catalog.PricingModeFixed),
	}
	pr := cartRollupProducts{products: products, hideNonActive: true}
	vr := cartRollupVariants{variants: variants}
	cr := cartRollupComponents{}
	src := appPricing.UnitPriceSources{Prices: prices, Variants: vr, Products: pr, Components: cr}
	pipeline := pricing.NewPipeline(
		appPricing.NewBasePriceStep(src),
		pricing.NewFinalizeStep(),
	)
	svc := cartApp.NewService(newStubCartRepo(), prices, nil, nil, vr, pr, cr, pipeline, testLogger(), testBus(), nil, nil)
	ctx := context.Background()

	cart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, cart.ID, "", "arch-v", 2, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem archived-to-be: %v", err)
	}
	if _, err := svc.AddItem(ctx, cart.ID, "", "live-v", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem live: %v", err)
	}
	products["arch"].Status = catalog.StatusArchived

	if _, err := svc.AddItem(ctx, cart.ID, "", "arch-v", 1, cartApp.AddItemOptions{}); err == nil {
		t.Fatal("AddItem increase after archive succeeded")
	}

	decreased, err := svc.UpdateItemQuantity(ctx, cart.ID, "", "arch-v", 1)
	if err != nil {
		t.Fatalf("decrease archived line: %v", err)
	}
	if len(decreased.Items) != 2 {
		t.Fatalf("items after decrease = %+v, want both lines", decreased.Items)
	}
	for _, item := range decreased.Items {
		if item.VariantID == "arch-v" && (item.Quantity != 1 || item.UnitPrice.Amount() != 1500) {
			t.Fatalf("archived line = %+v, want qty 1 price 1500", item)
		}
	}

	removed, err := svc.RemoveItem(ctx, cart.ID, "", "live-v")
	if err != nil {
		t.Fatalf("remove other line: %v", err)
	}
	if len(removed.Items) != 1 || removed.Items[0].VariantID != "arch-v" || removed.Items[0].UnitPrice.Amount() != 1500 {
		t.Fatalf("items after remove = %+v, want archived line at 1500", removed.Items)
	}
}

func TestService_AddItem_DynamicBundleStoreOnlyPrice(t *testing.T) {
	prices := newStubPriceRepo()
	prices.setWithStore("comp-v", "EUR", "store-1", 640)
	variants := map[string]catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
		"comp-v":   {ID: "comp-v", ProductID: "comp"},
	}
	products := map[string]*catalog.Product{
		"bundle": rollupProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic),
		"comp":   rollupProduct("comp", catalog.TypeSimple, catalog.PricingModeFixed),
	}
	components := []catalog.BundleComponent{{
		BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 1, Required: true,
	}}
	svc := newRollupService(prices, variants, products, components)
	noGlobalPrice := sellableSnap(catalog.DefaultVisibilityAxes())
	noGlobalPrice.Inputs.HasPrice = false
	facts := purchasableFacts{
		"bundle": noGlobalPrice,
		"comp":   sellableSnap(catalog.DefaultVisibilityAxes()),
	}
	svc = svc.WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	ctx := store.WithStore(context.Background(), &store.Store{ID: "store-1"})
	cart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	got, err := svc.AddItem(ctx, cart.ID, "", "bundle-v", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem store-only component price: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].UnitPrice.Amount() != 640 {
		t.Fatalf("item = %+v, want unit price 640", got.Items)
	}

	outOfStock := noGlobalPrice
	outOfStock.Inputs.QuantityPositive = false
	facts["bundle"] = outOfStock
	cart2, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, cart2.ID, "", "bundle-v", 1, cartApp.AddItemOptions{}); err == nil {
		t.Fatal("AddItem succeeded without stock")
	}
}

func TestNewService_RequiresUnitPriceSources(t *testing.T) {
	prices := newStubPriceRepo()
	defer func() {
		if recover() == nil {
			t.Fatal("nil bundle components did not panic")
		}
	}()
	cartApp.NewService(newStubCartRepo(), prices, nil, nil, cartRollupVariants{}, cartRollupProducts{}, nil, pricing.NewPipeline(), testLogger(), testBus(), nil, nil)
}
