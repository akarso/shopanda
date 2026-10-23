package checkout_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/testutil"
)

func idleProducts() catalog.ProductRepository { return &mockProductRepo047{} }

func idleComponents() catalog.BundleComponentRepository { return explodeComponentRepo{} }

func idlePrices() pricing.PriceRepository { return explodePriceRepo{} }

func TestNewCreateOrderStep_PanicsOnNilCatalog(t *testing.T) {
	orders := &mockOrderRepo{}
	variants := &mockVariantRepo037{variants: variantMap037("v1")}
	products := idleProducts()
	components := idleComponents()
	prices := idlePrices()
	cases := []struct {
		name string
		call func()
	}{
		{name: "products", call: func() { checkout.NewCreateOrderStep(orders, variants, nil, nil, nil, components, prices) }},
		{name: "components", call: func() { checkout.NewCreateOrderStep(orders, variants, nil, nil, products, nil, prices) }},
		{name: "prices", call: func() { checkout.NewCreateOrderStep(orders, variants, nil, nil, products, components, nil) }},
		{name: "variants", call: func() { checkout.NewCreateOrderStep(orders, nil, nil, nil, products, components, prices) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			tc.call()
		})
	}
}

func TestCreateOrderStep_ExplodeFalseKeepsParentLine(t *testing.T) {
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle": explodeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic, catalog.StockModeDynamic, false),
	}}
	variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"vb": explodeVariant(t, "vb", "bundle", "Bundle"),
	}}
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, explodeComponentRepo{}, explodePriceRepo{})

	cctx := explodeCheckout(t, "vb", 2, 1000)
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	items := cctx.Order.Items()
	if len(items) != 1 || items[0].VariantID != "vb" || items[0].Quantity != 2 || items[0].UnitPrice.Amount() != 1000 {
		t.Fatalf("items = %+v, want one parent line vb qty 2 at 1000", items)
	}
	if items[0].Restockable {
		t.Fatal("dynamic-stock bundle parent was restockable")
	}
}

func TestCreateOrderStep_FixedStockBundleStaysRestockable(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, false, catalog.PricingModeFixed, catalog.StockModeFixed)
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
	cctx := explodeCheckout(t, "vb", 1, 1000)
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	items := cctx.Order.Items()
	if len(items) != 1 || items[0].VariantID != "vb" || !items[0].Restockable {
		t.Fatalf("items = %+v, want one restockable parent line", items)
	}
}

func TestCreateOrderStep_ExplodeDynamicWritesLeafLines(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	calls := 0
	prices.batchCalls = &calls
	reservations := &mockReservationRepo{}
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices).
		WithReservations(reservations)

	// Leaf A qty 2 * cart 2 = 4 at 300. Leaf B (virtual) qty 1 * cart 2 = 2 at 400.
	// Rollup unit 1000, cart total 2000. Only the simple leaf is reserved.
	cctx := explodeCheckout(t, "vb", 2, 1000)
	cctx.SetMeta("reserved", true)
	cctx.SetMeta("reservations", []string{"res-va"})
	cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"va": 4})
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("batch price lookups = %d, want 1", calls)
	}
	if len(reservations.released) != 0 {
		t.Fatalf("released = %v, want none on success", reservations.released)
	}
	items := cctx.Order.Items()
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2 leaf lines", items)
	}
	byID := indexOrderItems(t, items)
	if parent, ok := byID["vb"]; ok {
		t.Fatalf("parent line was kept: %+v", parent)
	}
	a := byID["va"]
	if a.Quantity != 4 || a.UnitPrice.Amount() != 300 || a.SKU != "SKU-va" || !a.Restockable {
		t.Fatalf("leaf A = %+v, want qty 4 at 300 restockable", a)
	}
	b := byID["vb-leaf"]
	if b.Quantity != 2 || b.UnitPrice.Amount() != 400 || b.Restockable {
		t.Fatalf("virtual leaf = %+v, want qty 2 at 400 not restockable", b)
	}
	if cctx.Order.TotalAmount.Amount() != 2000 {
		t.Fatalf("order total = %d, want 2000", cctx.Order.TotalAmount.Amount())
	}
}

func TestCreateOrderStep_ExplodeRejectsTotalMismatch(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
	// Component rollup is 1000. A snapshot unit of 900 must fail closed.
	cctx := explodeCheckout(t, "vb", 2, 900)
	err := step.Execute(context.Background(), cctx)
	assertOrderLinesValidation(t, err, "does not match cart line total")
}

func TestCreateOrderStep_ExplodeInclusiveTaxMatchesPreTaxTotal(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
	// Component rollup is 2000. Inclusive tax has already reduced Total to net.
	cctx := explodeCheckout(t, "vb", 2, 1000)
	raw, _ := cctx.GetMeta("pricing")
	pctx := raw.(*pricing.PricingContext)
	tax, err := shared.NewMoney(200, "EUR")
	if err != nil {
		t.Fatalf("tax: %v", err)
	}
	adj, err := pricing.NewAdjustment(pricing.AdjustmentTax, "tax.DE.standard", tax)
	if err != nil {
		t.Fatalf("adjustment: %v", err)
	}
	adj.Included = true
	pctx.Items[0].Total = pctx.Items[0].Total.Sub(tax)
	pctx.Items[0].Adjustments = append(pctx.Items[0].Adjustments, adj)
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if cctx.Order.TotalAmount.Amount() != 2000 {
		t.Fatalf("order total = %d, want 2000 catalog rollup", cctx.Order.TotalAmount.Amount())
	}
}

func TestCreateOrderStep_ExplodeExclusiveTaxStillUsesItemTotal(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
	cctx := explodeCheckout(t, "vb", 2, 1000)
	raw, _ := cctx.GetMeta("pricing")
	pctx := raw.(*pricing.PricingContext)
	tax, err := shared.NewMoney(200, "EUR")
	if err != nil {
		t.Fatalf("tax: %v", err)
	}
	adj, err := pricing.NewAdjustment(pricing.AdjustmentTax, "tax.DE.standard", tax)
	if err != nil {
		t.Fatalf("adjustment: %v", err)
	}
	pctx.Items[0].Adjustments = append(pctx.Items[0].Adjustments, adj)
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if cctx.Order.TotalAmount.Amount() != 2000 {
		t.Fatalf("order total = %d, want 2000", cctx.Order.TotalAmount.Amount())
	}
}

func TestCreateOrderStep_ExplodeRejectsFixedModes(t *testing.T) {
	cases := []struct {
		name    string
		pricing catalog.PricingMode
		stock   catalog.StockMode
	}{
		{name: "fixed price", pricing: catalog.PricingModeFixed, stock: catalog.StockModeDynamic},
		{name: "fixed stock", pricing: catalog.PricingModeDynamic, stock: catalog.StockModeFixed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			products, variants, components, prices := explodeBundleCatalog(t, true, tc.pricing, tc.stock)
			step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
			cctx := explodeCheckout(t, "vb", 2, 1000)
			err := step.Execute(context.Background(), cctx)
			if err == nil || !strings.Contains(err.Error(), catalog.ExplodeOrderLinesRequiresDynamicMessage) {
				t.Fatalf("Execute error = %v", err)
			}
		})
	}
}

func TestCreateOrderStep_ExplodeMergesDuplicateVariants(t *testing.T) {
	// Standalone va qty 1 at 300, plus two exploded bundles that both contain va.
	// Bundle 1: va qty 1 * cart 2 = 2 at 300, and virtual vb-leaf qty 1 * 2 = 2 at 400.
	// Bundle 2: va qty 2 * cart 1 = 2 at 300.
	// Merged va qty = 1+2+2 = 5 at 300. Virtual stays 2 at 400.
	leafA := explodeProduct("leaf-a", catalog.TypeSimple, catalog.PricingModeFixed, catalog.StockModeFixed, false)
	leafB := explodeProduct("leaf-b", catalog.TypeVirtual, catalog.PricingModeFixed, catalog.StockModeFixed, false)
	bundle1 := explodeProduct("bundle-1", catalog.TypeBundle, catalog.PricingModeDynamic, catalog.StockModeDynamic, true)
	bundle2 := explodeProduct("bundle-2", catalog.TypeBundle, catalog.PricingModeDynamic, catalog.StockModeDynamic, true)
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"leaf-a": leafA, "leaf-b": leafB, "bundle-1": bundle1, "bundle-2": bundle2,
	}}
	variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"va":      explodeVariant(t, "va", "leaf-a", "Leaf A"),
		"vb-leaf": explodeVariant(t, "vb-leaf", "leaf-b", "Leaf B"),
		"b1":      explodeVariant(t, "b1", "bundle-1", "Bundle 1"),
		"b2":      explodeVariant(t, "b2", "bundle-2", "Bundle 2"),
	}}
	components := explodeComponentRepo{rows: []catalog.BundleComponent{
		mustComponent(t, "bundle-1", "leaf-a", 1),
		mustComponent(t, "bundle-1", "leaf-b", 1),
		mustComponent(t, "bundle-2", "leaf-a", 2),
	}}
	prices := explodePriceRepo{byVariant: map[string]int64{"va": 300, "vb-leaf": 400}}
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cart := mustExplodeCart(t)
	if err := cart.AddItem("va", 1, shared.MustNewMoney(300, "EUR")); err != nil {
		t.Fatalf("AddItem va: %v", err)
	}
	if err := cart.AddItem("b1", 2, shared.MustNewMoney(700, "EUR")); err != nil {
		t.Fatalf("AddItem b1: %v", err)
	}
	if err := cart.AddItem("b2", 1, shared.MustNewMoney(600, "EUR")); err != nil {
		t.Fatalf("AddItem b2: %v", err)
	}
	cctx.Cart = &cart
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	for _, spec := range []struct {
		id  string
		qty int
		amt int64
	}{
		{"va", 1, 300},
		{"b1", 2, 700},
		{"b2", 1, 600},
	} {
		pi, err := pricing.NewPricingItem(spec.id, spec.qty, shared.MustNewMoney(spec.amt, "EUR"))
		if err != nil {
			t.Fatalf("NewPricingItem: %v", err)
		}
		pctx.Items = append(pctx.Items, pi)
	}
	cctx.SetMeta("pricing", &pctx)

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	items := cctx.Order.Items()
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2 merged lines", items)
	}
	byID := indexOrderItems(t, items)
	if byID["va"].Quantity != 5 || byID["va"].UnitPrice.Amount() != 300 {
		t.Fatalf("merged va = %+v, want qty 5 at 300", byID["va"])
	}
	if byID["vb-leaf"].Quantity != 2 || byID["vb-leaf"].UnitPrice.Amount() != 400 {
		t.Fatalf("virtual = %+v, want qty 2 at 400", byID["vb-leaf"])
	}
	if _, ok := byID["b1"]; ok {
		t.Fatal("bundle parent b1 was kept")
	}
	if _, ok := byID["b2"]; ok {
		t.Fatal("bundle parent b2 was kept")
	}
}

func TestCreateOrderStep_ExplodeRejectsConflictingUnitPrice(t *testing.T) {
	leaf := explodeProduct("leaf-a", catalog.TypeSimple, catalog.PricingModeFixed, catalog.StockModeFixed, false)
	bundle := explodeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic, catalog.StockModeDynamic, true)
	products := &mockProductRepo047{products: map[string]*catalog.Product{"leaf-a": leaf, "bundle": bundle}}
	variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"va": explodeVariant(t, "va", "leaf-a", "Leaf A"),
		"vb": explodeVariant(t, "vb", "bundle", "Bundle"),
	}}
	components := explodeComponentRepo{rows: []catalog.BundleComponent{mustComponent(t, "bundle", "leaf-a", 1)}}
	prices := explodePriceRepo{byVariant: map[string]int64{"va": 300}}
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cart := mustExplodeCart(t)
	if err := cart.AddItem("va", 1, shared.MustNewMoney(500, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if err := cart.AddItem("vb", 1, shared.MustNewMoney(300, "EUR")); err != nil {
		t.Fatalf("AddItem bundle: %v", err)
	}
	cctx.Cart = &cart
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	standalone, err := pricing.NewPricingItem("va", 1, shared.MustNewMoney(500, "EUR"))
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	parent, err := pricing.NewPricingItem("vb", 1, shared.MustNewMoney(300, "EUR"))
	if err != nil {
		t.Fatalf("NewPricingItem bundle: %v", err)
	}
	pctx.Items = []pricing.PricingItem{standalone, parent}
	cctx.SetMeta("pricing", &pctx)

	err = step.Execute(context.Background(), cctx)
	assertOrderLinesValidation(t, err, "conflicting unit prices")
}

func explodeBundleCatalog(t *testing.T, explode bool, pricingMode catalog.PricingMode, stockMode catalog.StockMode) (*mockProductRepo047, *mockVariantRepo037, explodeComponentRepo, explodePriceRepo) {
	t.Helper()
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle": explodeProduct("bundle", catalog.TypeBundle, pricingMode, stockMode, explode),
		"leaf-a": explodeProduct("leaf-a", catalog.TypeSimple, catalog.PricingModeFixed, catalog.StockModeFixed, false),
		"leaf-b": explodeProduct("leaf-b", catalog.TypeVirtual, catalog.PricingModeFixed, catalog.StockModeFixed, false),
	}}
	variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"vb":      explodeVariant(t, "vb", "bundle", "Bundle"),
		"va":      explodeVariant(t, "va", "leaf-a", "Leaf A"),
		"vb-leaf": explodeVariant(t, "vb-leaf", "leaf-b", "Leaf B"),
	}}
	components := explodeComponentRepo{rows: []catalog.BundleComponent{
		mustComponent(t, "bundle", "leaf-a", 2),
		mustComponent(t, "bundle", "leaf-b", 1),
	}}
	prices := explodePriceRepo{byVariant: map[string]int64{"va": 300, "vb-leaf": 400}}
	return products, variants, components, prices
}

func explodeProduct(id string, typ catalog.Type, pricingMode catalog.PricingMode, stockMode catalog.StockMode, explode bool) *catalog.Product {
	return &catalog.Product{
		ID:                id,
		Name:              id,
		Slug:              id,
		Status:            catalog.StatusActive,
		Type:              typ,
		PricingMode:       pricingMode,
		StockMode:         stockMode,
		ExplodeOrderLines: explode,
		VisibilityModes:   catalog.DefaultVisibilityAxes(),
	}
}

func explodeVariant(t *testing.T, id, productID, name string) *catalog.Variant {
	t.Helper()
	v, err := catalog.NewVariant(id, productID, "SKU-"+id)
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	v.Name = name
	return &v
}

func mustComponent(t *testing.T, bundleID, componentID string, qty int) catalog.BundleComponent {
	t.Helper()
	c, err := catalog.NewBundleComponent(bundleID, componentID, qty)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	return *c
}

func explodeCheckout(t *testing.T, variantID string, qty int, unit int64) *checkout.Context {
	t.Helper()
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cart := mustExplodeCart(t)
	if err := cart.AddItem(variantID, qty, shared.MustNewMoney(unit, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	cctx.Cart = &cart
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	pi, err := pricing.NewPricingItem(variantID, qty, shared.MustNewMoney(unit, "EUR"))
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx.Items = []pricing.PricingItem{pi}
	cctx.SetMeta("pricing", &pctx)
	return cctx
}

func mustExplodeCart(t *testing.T) cart.Cart {
	t.Helper()
	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	return c
}

type explodeComponentRepo struct {
	rows []catalog.BundleComponent
}

func (r explodeComponentRepo) ListByBundleID(_ context.Context, bundleID string) ([]catalog.BundleComponent, error) {
	out := make([]catalog.BundleComponent, 0)
	for _, row := range r.rows {
		if row.BundleProductID == bundleID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (explodeComponentRepo) CountByBundleID(context.Context, string) (int, error) { return 0, nil }
func (explodeComponentRepo) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (explodeComponentRepo) Upsert(context.Context, *catalog.BundleComponent) error { return nil }
func (explodeComponentRepo) Remove(context.Context, string, string) error           { return nil }

type explodePriceRepo struct {
	byVariant  map[string]int64
	batchCalls *int
}

func (r explodePriceRepo) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, _ string) (*pricing.Price, error) {
	amount, ok := r.byVariant[variantID]
	if !ok {
		return nil, nil
	}
	price, err := pricing.NewPrice("price-"+variantID, variantID, "", shared.MustNewMoney(amount, currency))
	if err != nil {
		return nil, err
	}
	return &price, nil
}

func (r explodePriceRepo) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*pricing.Price, error) {
	if r.batchCalls != nil {
		*r.batchCalls++
	}
	return testutil.FindByVariantsCurrencyAndStoreFromFind(ctx, r.FindByVariantCurrencyAndStore, variantIDs, currency, storeID)
}

func (explodePriceRepo) ListByVariantID(context.Context, string) ([]pricing.Price, error) {
	return nil, nil
}
func (explodePriceRepo) List(context.Context, int, int) ([]pricing.Price, error) { return nil, nil }
func (explodePriceRepo) Upsert(context.Context, *pricing.Price) error            { return nil }

func indexOrderItems(t *testing.T, items []order.Item) map[string]order.Item {
	t.Helper()
	out := make(map[string]order.Item, len(items))
	for _, item := range items {
		out[item.VariantID] = item
	}
	return out
}

func assertOrderLinesValidation(t *testing.T, err error, detail string) {
	t.Helper()
	var appErr *apperror.Error
	if err == nil || !errors.As(err, &appErr) {
		t.Fatalf("err = %v, want validation", err)
	}
	if appErr.Code != apperror.CodeValidation {
		t.Fatalf("code = %s, want validation (err %v)", appErr.Code, err)
	}
	if appErr.Code == apperror.CodeInternal {
		t.Fatalf("explode failure was internal: %v", err)
	}
	if appErr.Message != checkout.OrderLinesCouldNotBeCreatedMessage {
		t.Fatalf("message = %q, want %q", appErr.Message, checkout.OrderLinesCouldNotBeCreatedMessage)
	}
	if strings.Contains(appErr.Message, "vb") || strings.Contains(appErr.Message, "va") {
		t.Fatalf("message %q contains a variant id", appErr.Message)
	}
	if appErr.Unwrap() == nil || !strings.Contains(appErr.Unwrap().Error(), detail) {
		t.Fatalf("unwrap = %v, want detail %q", appErr.Unwrap(), detail)
	}
}

func TestCreateOrderStep_MissingPricingIsValidation(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, false, catalog.PricingModeFixed, catalog.StockModeFixed)
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
	cctx := explodeCheckout(t, "vb", 1, 1000)
	raw, _ := cctx.GetMeta("pricing")
	raw.(*pricing.PricingContext).Items = nil
	err := step.Execute(context.Background(), cctx)
	assertOrderLinesValidation(t, err, "no pricing for variant")
}

func TestCreateOrderStep_ReleasePublishesStockAndLogsFailure(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	reservations := &mockReservationRepo{}
	bus := event.NewBus(logger.New("error"))
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices).
		WithReservations(reservations).
		WithStockEventPublishing(bus, logger.NewWithWriter(io.Discard, "info"))
	var productIDs []string
	var deltas []int
	bus.On(inventory.EventStockUpdated, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(inventory.StockUpdatedData)
		if !ok {
			t.Fatalf("data type %T", evt.Data)
		}
		productIDs = append(productIDs, data.ProductID)
		if data.Delta != nil {
			deltas = append(deltas, *data.Delta)
		}
		if evt.Source != "checkout.create_order" {
			t.Fatalf("source = %q", evt.Source)
		}
		return nil
	})
	cctx := explodeCheckout(t, "vb", 2, 1000)
	cctx.SetMeta("reserved", true)
	cctx.SetMeta("reservations", []string{"res-va"})
	cctx.SetMeta(checkout.ReservationHoldsMeta, []checkout.ReservationHold{{
		ID:              "res-va",
		VariantID:       "va",
		Quantity:        4,
		BundleProductID: "bundle",
	}})
	cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"swapped": 4})
	err := step.Execute(context.Background(), cctx)
	assertOrderLinesValidation(t, err, "do not match the quantities reserve held")
	if len(reservations.released) != 1 || reservations.released[0] != "res-va" {
		t.Fatalf("released = %v", reservations.released)
	}
	seenVariant := false
	seenParent := false
	for _, id := range productIDs {
		if id == "leaf-a" {
			seenVariant = true
		}
		if id == "bundle" {
			seenParent = true
		}
	}
	if !seenVariant || !seenParent {
		t.Fatalf("product ids = %v, want leaf-a and bundle", productIDs)
	}
	if len(deltas) != 1 || deltas[0] != 4 {
		t.Fatalf("deltas = %v, want [4]", deltas)
	}
	if reserved, _ := cctx.GetMeta("reserved"); reserved != false {
		t.Fatalf("reserved = %#v, want false after release", reserved)
	}

	reservations.releaseErr = errors.New("release down")
	reservations.released = nil
	var logBuf bytes.Buffer
	step = checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices).
		WithReservations(reservations).
		WithStockEventPublishing(bus, logger.NewWithWriter(&logBuf, "info"))
	cctx = explodeCheckout(t, "vb", 2, 1000)
	cctx.SetMeta("reserved", true)
	cctx.SetMeta("reservations", []string{"res-va"})
	cctx.SetMeta(checkout.ReservationHoldsMeta, []checkout.ReservationHold{{
		ID:        "res-va",
		VariantID: "va",
		Quantity:  4,
	}})
	cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"swapped": 4})
	if err := step.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected reservation mismatch")
	}
	if !strings.Contains(logBuf.String(), "checkout: release reservation failed") {
		t.Fatalf("log = %q, want release failure", logBuf.String())
	}
	if reserved, _ := cctx.GetMeta("reserved"); reserved != false {
		t.Fatalf("reserved = %#v, want false after a failed release", reserved)
	}
	rawHolds, _ := cctx.GetMeta(checkout.ReservationHoldsMeta)
	holds, _ := rawHolds.([]checkout.ReservationHold)
	if len(holds) != 1 || holds[0].ID != "res-va" {
		t.Fatalf("holds = %+v, want the hold release did not clear", holds)
	}
}

func TestCreateOrderStep_ReleaseWithoutHoldsSkipsEmptyStockEvent(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	reservations := &mockReservationRepo{}
	bus := event.NewBus(logger.New("error"))
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices).
		WithReservations(reservations).
		WithStockEventPublishing(bus, logger.NewWithWriter(io.Discard, "info"))
	published := 0
	bus.On(inventory.EventStockUpdated, func(_ context.Context, evt event.Event) error {
		published++
		data, ok := evt.Data.(inventory.StockUpdatedData)
		if !ok {
			t.Fatalf("data type %T", evt.Data)
		}
		if data.VariantID == "" || data.ProductID == "" || data.Delta == nil || *data.Delta == 0 {
			t.Errorf("empty stock payload: %+v", data)
		}
		return nil
	})
	cctx := explodeCheckout(t, "vb", 2, 1000)
	cctx.SetMeta("reserved", true)
	cctx.SetMeta("reservations", []string{"res-va"})
	cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"swapped": 4})
	err := step.Execute(context.Background(), cctx)
	assertOrderLinesValidation(t, err, "do not match the quantities reserve held")
	if len(reservations.released) != 1 || reservations.released[0] != "res-va" {
		t.Fatalf("released = %v, want [res-va]", reservations.released)
	}
	if published != 0 {
		t.Fatalf("published %d stock events, want 0", published)
	}
}

func TestCreateOrderStep_MissingProductIsValidation(t *testing.T) {
	products := &mockProductRepo047{products: map[string]*catalog.Product{"gone": nil}}
	variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"vb": explodeVariant(t, "vb", "gone", "Bundle"),
	}}
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, idleComponents(), idlePrices())
	err := step.Execute(context.Background(), explodeCheckout(t, "vb", 1, 1000))
	assertOrderLinesValidation(t, err, "product gone not found")
}

func TestCreateOrderStep_ExplodeCurrencyMismatchIsValidation(t *testing.T) {
	products, variants, components, _ := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, usdExplodePrices{})
	err := step.Execute(context.Background(), explodeCheckout(t, "vb", 2, 1000))
	assertOrderLinesValidation(t, err, "currency mismatch")
}

func TestCreateOrderStep_ReservedVariantMismatchIsValidation(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	reservations := &mockReservationRepo{}
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices).
		WithReservations(reservations)
	cctx := explodeCheckout(t, "vb", 2, 1000)
	cctx.SetMeta("reserved", true)
	cctx.SetMeta("reservations", []string{"res-swapped"})
	cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"swapped": 4})
	err := step.Execute(context.Background(), cctx)
	assertOrderLinesValidation(t, err, "do not match the quantities reserve held")
	if cctx.Order != nil {
		t.Fatal("order was saved after a reservation mismatch")
	}
	if len(reservations.released) != 1 || reservations.released[0] != "res-swapped" {
		t.Fatalf("released = %v, want [res-swapped]", reservations.released)
	}
}

func TestCreateOrderStep_ReservedSetMatchesWhatReserveHeld(t *testing.T) {
	t.Run("kept virtual is reserved with the exploded leaf", func(t *testing.T) {
		products, variants, components, prices := explodeBundlePlus(t, explodeExtra{id: "vv", productID: "virt", name: "Virtual", typ: catalog.TypeVirtual})
		step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
		cctx := explodeCartContext(t, []explodeLine{{id: "vb", qty: 2, unit: 1000}, {id: "vv", qty: 1, unit: 100}})
		cctx.SetMeta("reserved", true)
		cctx.SetMeta("reservations", []string{"res-va", "res-vv"})
		cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"va": 4, "vv": 1})
		if err := step.Execute(context.Background(), cctx); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		byID := indexOrderItems(t, cctx.Order.Items())
		if _, ok := byID["vb"]; ok {
			t.Fatal("exploded parent was kept")
		}
		if byID["vv"].Restockable {
			t.Fatal("virtual line was restockable")
		}
	})

	t.Run("omitting the kept virtual fails and releases", func(t *testing.T) {
		products, variants, components, prices := explodeBundlePlus(t, explodeExtra{id: "vv", productID: "virt", name: "Virtual", typ: catalog.TypeVirtual})
		reservations := &mockReservationRepo{}
		step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices).
			WithReservations(reservations)
		cctx := explodeCartContext(t, []explodeLine{{id: "vb", qty: 2, unit: 1000}, {id: "vv", qty: 1, unit: 100}})
		cctx.SetMeta("reserved", true)
		cctx.SetMeta("reservations", []string{"res-va", "res-vv"})
		cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"va": 4})
		err := step.Execute(context.Background(), cctx)
		assertOrderLinesValidation(t, err, "do not match the quantities reserve held")
		if len(reservations.released) != 2 {
			t.Fatalf("released = %v, want both reservation ids", reservations.released)
		}
	})

	t.Run("kept simple physical is part of the set", func(t *testing.T) {
		products, variants, components, prices := explodeBundlePlus(t, explodeExtra{id: "vs", productID: "plain", name: "Plain", typ: catalog.TypeSimple})
		step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
		cctx := explodeCartContext(t, []explodeLine{{id: "vb", qty: 2, unit: 1000}, {id: "vs", qty: 1, unit: 250}})
		cctx.SetMeta("reserved", true)
		cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"vs": 1, "va": 4})
		if err := step.Execute(context.Background(), cctx); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !indexOrderItems(t, cctx.Order.Items())["vs"].Restockable {
			t.Fatal("simple line was not restockable")
		}
	})

	t.Run("non-exploded dynamic bundle expects its leaf", func(t *testing.T) {
		products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
		products.products["leaf-c"] = explodeProduct("leaf-c", catalog.TypeSimple, catalog.PricingModeFixed, catalog.StockModeFixed, false)
		products.products["bundle-2"] = explodeProduct("bundle-2", catalog.TypeBundle, catalog.PricingModeDynamic, catalog.StockModeDynamic, false)
		variants.variants["vc"] = explodeVariant(t, "vc", "leaf-c", "Leaf C")
		variants.variants["b2"] = explodeVariant(t, "b2", "bundle-2", "Bundle 2")
		components.rows = append(components.rows, mustComponent(t, "bundle-2", "leaf-c", 1))
		step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
		cctx := explodeCartContext(t, []explodeLine{{id: "vb", qty: 2, unit: 1000}, {id: "b2", qty: 1, unit: 500}})
		cctx.SetMeta("reserved", true)
		cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"va": 4, "vc": 1})
		if err := step.Execute(context.Background(), cctx); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		byID := indexOrderItems(t, cctx.Order.Items())
		if _, ok := byID["b2"]; !ok {
			t.Fatal("non-exploded bundle parent was dropped")
		}
		if _, ok := byID["vc"]; ok {
			t.Fatal("non-exploded bundle was itemized")
		}
		if byID["b2"].Restockable {
			t.Fatal("non-exploded dynamic bundle parent was restockable")
		}
	})

	t.Run("reserving the non-exploded parent fails", func(t *testing.T) {
		products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
		products.products["leaf-c"] = explodeProduct("leaf-c", catalog.TypeSimple, catalog.PricingModeFixed, catalog.StockModeFixed, false)
		products.products["bundle-2"] = explodeProduct("bundle-2", catalog.TypeBundle, catalog.PricingModeDynamic, catalog.StockModeDynamic, false)
		variants.variants["vc"] = explodeVariant(t, "vc", "leaf-c", "Leaf C")
		variants.variants["b2"] = explodeVariant(t, "b2", "bundle-2", "Bundle 2")
		components.rows = append(components.rows, mustComponent(t, "bundle-2", "leaf-c", 1))
		step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
		cctx := explodeCartContext(t, []explodeLine{{id: "vb", qty: 2, unit: 1000}, {id: "b2", qty: 1, unit: 500}})
		cctx.SetMeta("reserved", true)
		cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"va": 4, "b2": 1})
		err := step.Execute(context.Background(), cctx)
		assertOrderLinesValidation(t, err, "do not match the quantities reserve held")
	})

	t.Run("same variant with the wrong quantity fails", func(t *testing.T) {
		products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
		step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
		cctx := explodeCheckout(t, "vb", 2, 1000)
		cctx.SetMeta("reserved", true)
		cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"va": 1})
		err := step.Execute(context.Background(), cctx)
		assertOrderLinesValidation(t, err, "do not match the quantities reserve held")
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.Unwrap() == nil {
			t.Fatalf("err = %v", err)
		}
		detail := appErr.Unwrap().Error()
		expected := strings.Index(detail, "map[va:4]")
		held := strings.Index(detail, "map[va:1]")
		if expected < 0 || held < 0 || expected > held {
			t.Fatalf("detail = %q, want expected qty before the qty reserve held", detail)
		}
	})
}

func TestCreateOrderStep_ExplodeSkipsParentAndSnapshotsKeptLine(t *testing.T) {
	products, variants, components, prices := explodeBundlePlus(t, explodeExtra{id: "vv", productID: "virt", name: "Virtual", typ: catalog.TypeVirtual})
	snap := &explodeSnapshotter{}
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, snap, products, components, prices)
	cctx := explodeCartContext(t, []explodeLine{{id: "vb", qty: 2, unit: 1000}, {id: "vv", qty: 1, unit: 100}})
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(snap.variantIDs) != 1 || snap.variantIDs[0] != "vv" {
		t.Fatalf("snapshotted %v, want [vv]", snap.variantIDs)
	}
}

type explodeLine struct {
	id   string
	qty  int
	unit int64
}

type explodeExtra struct {
	id        string
	productID string
	name      string
	typ       catalog.Type
}

func explodeBundlePlus(t *testing.T, extra explodeExtra) (*mockProductRepo047, *mockVariantRepo037, explodeComponentRepo, explodePriceRepo) {
	t.Helper()
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	products.products[extra.productID] = explodeProduct(extra.productID, extra.typ, catalog.PricingModeFixed, catalog.StockModeFixed, false)
	variants.variants[extra.id] = explodeVariant(t, extra.id, extra.productID, extra.name)
	return products, variants, components, prices
}

func explodeCartContext(t *testing.T, lines []explodeLine) *checkout.Context {
	t.Helper()
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cart := mustExplodeCart(t)
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	for _, line := range lines {
		if err := cart.AddItem(line.id, line.qty, shared.MustNewMoney(line.unit, "EUR")); err != nil {
			t.Fatalf("AddItem %s: %v", line.id, err)
		}
		pi, err := pricing.NewPricingItem(line.id, line.qty, shared.MustNewMoney(line.unit, "EUR"))
		if err != nil {
			t.Fatalf("NewPricingItem %s: %v", line.id, err)
		}
		pctx.Items = append(pctx.Items, pi)
	}
	cctx.Cart = &cart
	cctx.SetMeta("pricing", &pctx)
	return cctx
}

func TestCreateOrderStep_ExplodeUsesCartProductFromMeta(t *testing.T) {
	bundle := explodeProduct("bundle", catalog.TypeBundle, catalog.PricingModeDynamic, catalog.StockModeDynamic, true)
	_, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	// A live lookup of this repo returns a simple product, which would keep the parent line.
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, idleProducts(), components, prices)
	cctx := explodeCheckout(t, "vb", 2, 1000)
	cctx.SetMeta("cart_products", map[string]*catalog.Product{"bundle": bundle})
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := indexOrderItems(t, cctx.Order.Items())["vb"]; ok {
		t.Fatal("parent line was kept; cart product meta was not used")
	}
}

func TestCreateOrderStep_ExplodeSkipsParentExtensionSnapshot(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	snap := &explodeSnapshotter{}
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, snap, products, components, prices)
	if err := step.Execute(context.Background(), explodeCheckout(t, "vb", 2, 1000)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(snap.variantIDs) != 0 {
		t.Fatalf("snapshotted %v, want no parent snapshot", snap.variantIDs)
	}
}

type explodeSnapshotter struct {
	variantIDs []string
}

func (s *explodeSnapshotter) SnapshotCartItemToOrderItem(_ context.Context, _, _, variantID, _ string) error {
	s.variantIDs = append(s.variantIDs, variantID)
	return nil
}

type usdExplodePrices struct{}

func (usdExplodePrices) FindByVariantCurrencyAndStore(context.Context, string, string, string) (*pricing.Price, error) {
	return nil, nil
}
func (usdExplodePrices) FindByVariantsCurrencyAndStore(_ context.Context, variantIDs []string, _, _ string) (map[string]*pricing.Price, error) {
	out := make(map[string]*pricing.Price, len(variantIDs))
	for _, id := range variantIDs {
		price, err := pricing.NewPrice("price-"+id, id, "", shared.MustNewMoney(300, "USD"))
		if err != nil {
			return nil, err
		}
		out[id] = &price
	}
	return out, nil
}
func (usdExplodePrices) ListByVariantID(context.Context, string) ([]pricing.Price, error) {
	return nil, nil
}
func (usdExplodePrices) List(context.Context, int, int) ([]pricing.Price, error) { return nil, nil }
func (usdExplodePrices) Upsert(context.Context, *pricing.Price) error            { return nil }
