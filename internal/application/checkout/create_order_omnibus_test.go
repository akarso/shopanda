package checkout_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// countingPriceRepo is the price repository CreateOrderStep holds.
// Upsert does not write price_history. History is a separate repository,
// and CreateOrderStep does not take one.
type countingPriceRepo struct {
	explodePriceRepo
	upserts int
}

func (r *countingPriceRepo) Upsert(ctx context.Context, p *pricing.Price) error {
	r.upserts++
	return r.explodePriceRepo.Upsert(ctx, p)
}

func assertCreateOrderStepHasNoPriceHistory(t *testing.T) {
	t.Helper()
	hist := reflect.TypeOf((*pricing.PriceHistoryRepository)(nil)).Elem()
	typ := reflect.TypeOf(checkout.CreateOrderStep{})
	if typ.Kind() != reflect.Struct {
		t.Fatalf("CreateOrderStep kind = %s, want struct", typ.Kind())
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type == hist || f.Type.Implements(hist) {
			t.Fatalf("CreateOrderStep.%s type %s carries PriceHistoryRepository", f.Name, f.Type)
		}
	}
}

func TestCreateOrderStep_ExplodeDoesNotRecordPriceHistory(t *testing.T) {
	assertCreateOrderStepHasNoPriceHistory(t)

	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	repo := &countingPriceRepo{explodePriceRepo: prices}
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, repo)

	cctx := explodeCheckout(t, "vb", 2, 1000)
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.upserts != 0 {
		t.Fatalf("price upserts during CreateOrderStep = %d, want 0 (Upsert is not a history write)", repo.upserts)
	}

	items := indexOrderItems(t, cctx.Order.Items())
	if _, ok := items["vb"]; ok {
		t.Fatal("parent line was kept")
	}
	if items["va"].VariantID != "va" || items["vb-leaf"].VariantID != "vb-leaf" {
		t.Fatalf("items = %+v, want leaf lines va and vb-leaf", items)
	}
}

func TestCreateOrderStep_ExplodedOrderTaxStaysOnOrder(t *testing.T) {
	// Copies PricingContext.TaxTotal onto the order. This is not a VAT-rules check.
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	step := checkout.NewCreateOrderStep(&mockOrderRepo{}, variants, nil, nil, products, components, prices)
	cctx := explodeCheckout(t, "vb", 2, 1000)
	raw, _ := cctx.GetMeta("pricing")
	pctx := raw.(*pricing.PricingContext)
	pctx.TaxTotal = shared.MustNewMoney(380, "EUR")

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !cctx.Order.TaxAmount.Equal(pctx.TaxTotal) {
		t.Fatalf("TaxAmount = %d %s, want pipeline TaxTotal %d %s",
			cctx.Order.TaxAmount.Amount(), cctx.Order.TaxAmount.Currency(),
			pctx.TaxTotal.Amount(), pctx.TaxTotal.Currency())
	}
	// Leaf catalog prices: va 300 × (2 components × cart qty 2) + vb-leaf 400 × cart qty 2.
	if cctx.Order.TotalAmount.Amount() != 2000 || cctx.Order.TotalAmount.Currency() != "EUR" {
		t.Fatalf("TotalAmount = %d %s, want 2000 EUR", cctx.Order.TotalAmount.Amount(), cctx.Order.TotalAmount.Currency())
	}
	payable, err := cctx.Order.PayableAmount()
	if err != nil {
		t.Fatalf("PayableAmount: %v", err)
	}
	if payable.Amount() != 2380 || payable.Currency() != "EUR" {
		t.Fatalf("PayableAmount = %d %s, want 2380 EUR (order total 2000 + tax 380)", payable.Amount(), payable.Currency())
	}

	items := indexOrderItems(t, cctx.Order.Items())
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 leaf lines", len(items))
	}
	if !items["va"].Restockable {
		t.Fatal("simple leaf Restockable = false, want true")
	}
	if items["vb-leaf"].Restockable {
		t.Fatal("virtual leaf Restockable = true, want false")
	}
	for _, item := range items {
		assertOrderItemHasNoTaxField(t, item)
	}
}

func assertOrderItemHasNoTaxField(t *testing.T, item order.Item) {
	t.Helper()
	allowed := map[string]struct{}{
		"VariantID":   {},
		"SKU":         {},
		"Name":        {},
		"Quantity":    {},
		"UnitPrice":   {},
		"Restockable": {},
		"CreatedAt":   {},
	}
	typ := reflect.TypeOf(item)
	if typ.NumField() != len(allowed) {
		t.Fatalf("order item fields = %d, want %d (Restockable is the only extra)", typ.NumField(), len(allowed))
	}
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if _, ok := allowed[name]; !ok {
			t.Fatalf("unexpected order item field %s", name)
		}
	}
}
