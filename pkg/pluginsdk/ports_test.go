package pluginsdk_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	domainorder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

type stubProducts struct {
	byID map[string]*catalog.Product
}

func (s stubProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return s.byID[id], nil
}
func (stubProducts) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (stubProducts) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (stubProducts) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (stubProducts) Create(context.Context, *catalog.Product) error { return nil }
func (stubProducts) Update(context.Context, *catalog.Product) error { return nil }

type stubVariants struct {
	byID map[string]*catalog.Variant
}

func (s stubVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return s.byID[id], nil
}
func (stubVariants) FindBySKU(context.Context, string) (*catalog.Variant, error) { return nil, nil }
func (stubVariants) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (stubVariants) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (stubVariants) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (stubVariants) Create(context.Context, *catalog.Variant) error { return nil }
func (stubVariants) Update(context.Context, *catalog.Variant) error { return nil }

type stubOrders struct {
	byID map[string]*domainorder.Order
}

func (s stubOrders) FindByID(_ context.Context, id string) (*domainorder.Order, error) {
	return s.byID[id], nil
}
func (stubOrders) FindByIDs(context.Context, []string) (map[string]*domainorder.Order, error) {
	return nil, nil
}
func (stubOrders) FindByCustomerID(context.Context, string) ([]domainorder.Order, error) {
	return nil, nil
}
func (stubOrders) FindByContactEmail(context.Context, string) ([]domainorder.Order, error) {
	return nil, nil
}
func (stubOrders) List(context.Context, int, int) ([]domainorder.Order, error) { return nil, nil }
func (stubOrders) Save(context.Context, *domainorder.Order) error              { return nil }
func (stubOrders) UpdateStatus(context.Context, *domainorder.Order) error      { return nil }
func (stubOrders) LinkToCustomer(context.Context, *domainorder.Order) error    { return nil }
func (stubOrders) LinkToCustomerByContactEmail(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (stubOrders) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]domainorder.TaxSnapshotRow, error) {
	return nil, nil
}

func TestCatalogPort_Find(t *testing.T) {
	port := pluginsdk.NewCatalogPort(stubProducts{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Slug: "shirt", Name: "Shirt", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}, stubVariants{byID: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "M"},
	}})
	p, err := port.FindProduct(context.Background(), "p1")
	if err != nil || p == nil || p.Slug != "shirt" {
		t.Fatalf("FindProduct = %+v, err=%v", p, err)
	}
	v, err := port.FindVariant(context.Background(), "v1")
	if err != nil || v == nil || v.SKU != "SKU-1" {
		t.Fatalf("FindVariant = %+v, err=%v", v, err)
	}
	if v.ProductSlug != "shirt" {
		t.Fatalf("ProductSlug = %q, want shirt", v.ProductSlug)
	}
	batch, err := port.FindVariants(context.Background(), []string{"v1", "missing"})
	if err != nil || batch["v1"] == nil || batch["v1"].ProductSlug != "shirt" {
		t.Fatalf("FindVariants = %+v, err=%v", batch, err)
	}
	if _, ok := batch["missing"]; ok {
		t.Fatal("missing id must be omitted")
	}
}

func TestOrdersPort_FindByID(t *testing.T) {
	item, err := domainorder.NewItem("v1", "SKU", "Name", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := domainorder.NewOrder("o1", "c1", "", "EUR", []domainorder.Item{item})
	if err != nil {
		t.Fatal(err)
	}
	port := pluginsdk.NewOrdersPort(stubOrders{byID: map[string]*domainorder.Order{o.ID: &o}})
	got, err := port.FindByID(context.Background(), "o1")
	if err != nil || got == nil || got.ID != "o1" {
		t.Fatalf("FindByID = %+v, err=%v", got, err)
	}
	if got.Status == "" {
		t.Fatal("expected status on OrderView")
	}
	_ = extapi.OrderView{} // compile-time anchor for extapi shape
}
