package checkout_test

import (
	"context"
	"errors"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type qtyVariantRepo struct {
	variants map[string]catalog.Variant
}

func (r qtyVariantRepo) FindByID(_ context.Context, variantID string) (*catalog.Variant, error) {
	v, ok := r.variants[variantID]
	if !ok {
		return nil, nil
	}
	cp := v
	return &cp, nil
}
func (qtyVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) { return nil, nil }
func (qtyVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (r qtyVariantRepo) ListByProductID(_ context.Context, productID string, _, limit int) ([]catalog.Variant, error) {
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
func (r qtyVariantRepo) ListByProductIDs(_ context.Context, productIDs []string, limit int) (map[string][]catalog.Variant, error) {
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
func (qtyVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (qtyVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

type qtyComponentRepo struct {
	rows []catalog.BundleComponent
}

func (r qtyComponentRepo) ListByBundleID(_ context.Context, bundleProductID string) ([]catalog.BundleComponent, error) {
	out := make([]catalog.BundleComponent, 0)
	for _, row := range r.rows {
		if row.BundleProductID == bundleProductID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (qtyComponentRepo) CountByBundleID(context.Context, string) (int, error)    { return 0, nil }
func (qtyComponentRepo) CountByComponentID(context.Context, string) (int, error) { return 0, nil }
func (qtyComponentRepo) Upsert(context.Context, *catalog.BundleComponent) error  { return nil }
func (qtyComponentRepo) Remove(context.Context, string, string) error            { return nil }

type qtyStockRepo struct {
	qty map[string]int
}

func (m qtyStockRepo) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	return inventory.StockEntry{VariantID: variantID, Quantity: m.qty[variantID]}, nil
}
func (qtyStockRepo) SetStock(context.Context, *inventory.StockEntry) error   { return nil }
func (qtyStockRepo) SetStocks(context.Context, []inventory.StockEntry) error { return nil }
func (qtyStockRepo) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}
func (qtyStockRepo) ListInventory(context.Context, int, int, string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}
func (qtyStockRepo) GetInventoryItem(context.Context, string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}

func qtyProduct(id string, typ catalog.Type) *catalog.Product {
	return &catalog.Product{ID: id, Status: catalog.StatusActive, Type: typ, PricingMode: catalog.PricingModeFixed}
}

func cartWithLineQty(t *testing.T, variantID string, qty int) *cart.Cart {
	t.Helper()
	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem(variantID, qty, shared.MustNewMoney(1000, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	return &c
}

func executeValidateCart(t *testing.T, step *checkout.ValidateCartStep, c *cart.Cart) error {
	t.Helper()
	cctx := checkout.NewContext(c.ID, "cust-1", "EUR")
	cctx.Cart = c
	return step.Execute(context.Background(), cctx)
}

func TestValidateCartStep_RejectsDigitalOnlyLineAboveCap(t *testing.T) {
	bundle := qtyProduct("bundle", catalog.TypeBundle)
	bundle.StockMode = catalog.StockModeDynamic
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle":  bundle,
		"virtual": qtyProduct("virtual", catalog.TypeVirtual),
	}}
	variants := qtyVariantRepo{variants: map[string]catalog.Variant{
		"bundle-v":  {ID: "bundle-v", ProductID: "bundle"},
		"virtual-v": {ID: "virtual-v", ProductID: "virtual"},
	}}
	components := qtyComponentRepo{rows: []catalog.BundleComponent{{
		BundleProductID: "bundle", ComponentProductID: "virtual", Quantity: 1, Required: true,
	}}}
	step := checkout.NewValidateCartStep(variants, products, components, qtyStockRepo{qty: map[string]int{}})

	err := executeValidateCart(t, step, cartWithLineQty(t, "bundle-v", catalog.MaxDigitalBundleLineQty+100))
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Message != "only 100 available" {
		t.Fatalf("Execute err = %v, want only 100 available", err)
	}
}

func TestValidateCartStep_RejectsDynamicSimpleLeafAboveRollup(t *testing.T) {
	bundle := qtyProduct("bundle", catalog.TypeBundle)
	bundle.StockMode = catalog.StockModeDynamic
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle": bundle,
		"comp":   qtyProduct("comp", catalog.TypeSimple),
		"opt":    qtyProduct("opt", catalog.TypeSimple),
	}}
	variants := qtyVariantRepo{variants: map[string]catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
		"comp-v":   {ID: "comp-v", ProductID: "comp"},
		"opt-v":    {ID: "opt-v", ProductID: "opt"},
	}}
	components := qtyComponentRepo{rows: []catalog.BundleComponent{
		{BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 2, Required: true},
		{BundleProductID: "bundle", ComponentProductID: "opt", Quantity: 1, Required: false},
	}}
	stock := qtyStockRepo{qty: map[string]int{"bundle-v": 0, "comp-v": 3, "opt-v": 0}}
	step := checkout.NewValidateCartStep(variants, products, components, stock)

	err := executeValidateCart(t, step, cartWithLineQty(t, "bundle-v", 2))
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Message != "only 1 available" {
		t.Fatalf("Execute err = %v, want only 1 available (floor(3/2)=1; optional stock is ignored)", err)
	}
}

func TestValidateCartStep_AllowsQtyWithinAvailable(t *testing.T) {
	t.Run("digital cap", func(t *testing.T) {
		bundle := qtyProduct("bundle", catalog.TypeBundle)
		bundle.StockMode = catalog.StockModeDynamic
		products := &mockProductRepo047{products: map[string]*catalog.Product{
			"bundle":  bundle,
			"virtual": qtyProduct("virtual", catalog.TypeVirtual),
		}}
		variants := qtyVariantRepo{variants: map[string]catalog.Variant{
			"bundle-v":  {ID: "bundle-v", ProductID: "bundle"},
			"virtual-v": {ID: "virtual-v", ProductID: "virtual"},
		}}
		components := qtyComponentRepo{rows: []catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "virtual", Quantity: 1, Required: true,
		}}}
		step := checkout.NewValidateCartStep(variants, products, components, qtyStockRepo{qty: map[string]int{}})
		if err := executeValidateCart(t, step, cartWithLineQty(t, "bundle-v", catalog.MaxDigitalBundleLineQty)); err != nil {
			t.Fatalf("Execute qty %d: %v", catalog.MaxDigitalBundleLineQty, err)
		}
	})

	t.Run("simple leaf rollup", func(t *testing.T) {
		bundle := qtyProduct("bundle", catalog.TypeBundle)
		bundle.StockMode = catalog.StockModeDynamic
		products := &mockProductRepo047{products: map[string]*catalog.Product{
			"bundle": bundle,
			"comp":   qtyProduct("comp", catalog.TypeSimple),
		}}
		variants := qtyVariantRepo{variants: map[string]catalog.Variant{
			"bundle-v": {ID: "bundle-v", ProductID: "bundle"},
			"comp-v":   {ID: "comp-v", ProductID: "comp"},
		}}
		components := qtyComponentRepo{rows: []catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "comp", Quantity: 2, Required: true,
		}}}
		stock := qtyStockRepo{qty: map[string]int{"comp-v": 3}}
		step := checkout.NewValidateCartStep(variants, products, components, stock)
		if err := executeValidateCart(t, step, cartWithLineQty(t, "bundle-v", 1)); err != nil {
			t.Fatalf("Execute qty 1: %v", err)
		}
	})
}

func TestNewValidateCartStep_NilDepsPanic(t *testing.T) {
	variants := &mockVariantRepo{}
	products := &mockProductRepo047{}
	components := cartApp.EmptyBundleComponents()
	stock := cartApp.PermissiveStock()
	cases := []struct {
		name string
		call func()
	}{
		{"nil variants", func() { checkout.NewValidateCartStep(nil, products, components, stock) }},
		{"nil products", func() { checkout.NewValidateCartStep(variants, nil, components, stock) }},
		{"nil components", func() { checkout.NewValidateCartStep(variants, products, nil, stock) }},
		{"nil stock", func() { checkout.NewValidateCartStep(variants, products, components, nil) }},
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
