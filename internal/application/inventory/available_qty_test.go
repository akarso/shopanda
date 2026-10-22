package inventory_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	appinventory "github.com/akarso/shopanda/internal/application/inventory"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type qtyProducts struct {
	products map[string]*catalog.Product
}

func (m qtyProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return m.products[id], nil
}
func (qtyProducts) FindBySlug(context.Context, string) (*catalog.Product, error) { return nil, nil }
func (qtyProducts) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (qtyProducts) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (qtyProducts) Create(context.Context, *catalog.Product) error { return nil }
func (qtyProducts) Update(context.Context, *catalog.Product) error { return nil }

type qtyVariants struct {
	variants map[string]catalog.Variant
}

func (m qtyVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	v, ok := m.variants[id]
	if !ok {
		return nil, nil
	}
	return &v, nil
}
func (qtyVariants) FindBySKU(context.Context, string) (*catalog.Variant, error) { return nil, nil }
func (qtyVariants) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (m qtyVariants) ListByProductID(_ context.Context, productID string, _, limit int) ([]catalog.Variant, error) {
	var out []catalog.Variant
	for _, v := range m.variants {
		if v.ProductID == productID {
			out = append(out, v)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m qtyVariants) ListByProductIDs(_ context.Context, productIDs []string, limit int) (map[string][]catalog.Variant, error) {
	out := make(map[string][]catalog.Variant, len(productIDs))
	for _, id := range productIDs {
		list, _ := m.ListByProductID(context.Background(), id, 0, limit)
		out[id] = list
	}
	return out, nil
}
func (qtyVariants) Create(context.Context, *catalog.Variant) error { return nil }
func (qtyVariants) Update(context.Context, *catalog.Variant) error { return nil }

type qtyComponents struct {
	rows []catalog.BundleComponent
}

func (m qtyComponents) ListByBundleID(_ context.Context, bundleProductID string) ([]catalog.BundleComponent, error) {
	var out []catalog.BundleComponent
	for _, row := range m.rows {
		if row.BundleProductID == bundleProductID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (qtyComponents) CountByBundleID(context.Context, string) (int, error) { return 0, nil }
func (qtyComponents) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (qtyComponents) Upsert(context.Context, *catalog.BundleComponent) error { return nil }
func (qtyComponents) Remove(context.Context, string, string) error           { return nil }

type qtyStock struct {
	qty map[string]int
}

func (m qtyStock) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	return inventory.StockEntry{VariantID: variantID, Quantity: m.qty[variantID]}, nil
}
func (qtyStock) SetStock(context.Context, *inventory.StockEntry) error   { return nil }
func (qtyStock) SetStocks(context.Context, []inventory.StockEntry) error { return nil }
func (qtyStock) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}
func (qtyStock) ListInventory(context.Context, int, int, string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}
func (qtyStock) GetInventoryItem(context.Context, string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}

func TestResolveAvailableQty_DynamicDigitalLeavesDoNotGate(t *testing.T) {
	src := appinventory.AvailableQtySources{
		Stock: qtyStock{qty: map[string]int{"simple-v": 5, "bundle-v": 0}},
		Variants: qtyVariants{variants: map[string]catalog.Variant{
			"bundle-v":  {ID: "bundle-v", ProductID: "bundle"},
			"simple-v":  {ID: "simple-v", ProductID: "simple"},
			"virtual-v": {ID: "virtual-v", ProductID: "virtual"},
		}},
		Products: qtyProducts{products: map[string]*catalog.Product{
			"bundle":  {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
			"simple":  {ID: "simple", Type: catalog.TypeSimple, Status: catalog.StatusActive},
			"virtual": {ID: "virtual", Type: catalog.TypeVirtual, Status: catalog.StatusActive},
		}},
		Components: qtyComponents{rows: []catalog.BundleComponent{
			{BundleProductID: "bundle", ComponentProductID: "simple", Quantity: 2, Required: true},
			{BundleProductID: "bundle", ComponentProductID: "virtual", Quantity: 9, Required: true},
		}},
	}
	got, err := appinventory.ResolveAvailableQty(context.Background(), src, "bundle", "bundle-v")
	if err != nil {
		t.Fatalf("ResolveAvailableQty: %v", err)
	}
	if got != 2 {
		t.Fatalf("available = %d, want 2 (floor(5/2); virtual leaf does not gate)", got)
	}

	src.Components = qtyComponents{rows: []catalog.BundleComponent{
		{BundleProductID: "bundle", ComponentProductID: "virtual", Quantity: 3, Required: true},
	}}
	got, err = appinventory.ResolveAvailableQty(context.Background(), src, "bundle", "")
	if err != nil {
		t.Fatalf("virtual-only ResolveAvailableQty: %v", err)
	}
	if got != catalog.MaxDigitalBundleLineQty {
		t.Fatalf("available = %d, want digital-only line cap %d", got, catalog.MaxDigitalBundleLineQty)
	}
}

func TestRejectQtyAboveAvailable_KeepsMessageAndHidesEmptyErrorText(t *testing.T) {
	const secret = "secret-variant-id"
	base := appinventory.AvailableQtySources{
		Stock: qtyStock{},
		Variants: qtyVariants{variants: map[string]catalog.Variant{
			"v": {ID: "v", ProductID: "p"},
		}},
	}

	t.Run("keeps message", func(t *testing.T) {
		src := base
		src.Products = qtyErrProducts{err: apperror.NotFound("variant missing")}
		err := appinventory.RejectQtyAboveAvailable(context.Background(), src, "p", "v", 1)
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.Message != "variant missing" {
			t.Fatalf("err = %v, want message %q", err, "variant missing")
		}
	})

	t.Run("empty message", func(t *testing.T) {
		src := base
		src.Products = qtyErrProducts{err: apperror.Wrap(apperror.CodeValidation, "", fmt.Errorf("lookup %s failed", secret))}
		err := appinventory.RejectQtyAboveAvailable(context.Background(), src, "p", "v", 1)
		var appErr *apperror.Error
		if !errors.As(err, &appErr) {
			t.Fatalf("err = %v, want wrapped application error", err)
		}
		if appErr.Message != "available quantity" {
			t.Fatalf("message = %q, want %q", appErr.Message, "available quantity")
		}
		if strings.Contains(appErr.Message, secret) {
			t.Fatalf("message = %q, want secret kept off Message", appErr.Message)
		}
	})
}

func TestRejectQtyAboveAvailable_PreservesInfrastructureErrors(t *testing.T) {
	base := appinventory.AvailableQtySources{
		Stock: qtyStock{},
		Variants: qtyVariants{variants: map[string]catalog.Variant{
			"v": {ID: "v", ProductID: "p"},
		}},
	}

	t.Run("plain error", func(t *testing.T) {
		src := base
		src.Products = qtyErrProducts{err: fmt.Errorf("stock connection reset")}
		err := appinventory.RejectQtyAboveAvailable(context.Background(), src, "p", "v", 1)
		if err == nil {
			t.Fatal("expected error")
		}
		if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
			t.Fatalf("err = %v, want infrastructure error unchanged", err)
		}
		if !strings.Contains(err.Error(), "stock connection reset") {
			t.Fatalf("err = %v, want the original text", err)
		}
	})

	t.Run("internal", func(t *testing.T) {
		src := base
		src.Products = qtyErrProducts{err: apperror.Internal("db down")}
		err := appinventory.RejectQtyAboveAvailable(context.Background(), src, "p", "v", 1)
		if !apperror.Is(err, apperror.CodeInternal) {
			t.Fatalf("err = %v, want CodeInternal", err)
		}
		if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
			t.Fatalf("err = %v, want Is false for validation and not-found", err)
		}
	})
}

type qtyErrProducts struct {
	err error
}

func (m qtyErrProducts) FindByID(context.Context, string) (*catalog.Product, error) {
	return nil, m.err
}
func (qtyErrProducts) FindBySlug(context.Context, string) (*catalog.Product, error) { return nil, nil }
func (qtyErrProducts) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (qtyErrProducts) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (qtyErrProducts) Create(context.Context, *catalog.Product) error { return nil }
func (qtyErrProducts) Update(context.Context, *catalog.Product) error { return nil }
