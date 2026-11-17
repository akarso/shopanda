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

type batchVariants struct {
	stubVariants
	findByIDs int
}

func (b *batchVariants) FindByIDs(_ context.Context, ids []string) (map[string]*catalog.Variant, error) {
	b.findByIDs++
	out := make(map[string]*catalog.Variant, len(ids))
	for _, id := range ids {
		if v := b.byID[id]; v != nil {
			out[v.ID] = v
		}
	}
	return out, nil
}

type batchProducts struct {
	stubProducts
	listByIDs int
	err       error
}

func (b *batchProducts) ListByIDs(_ context.Context, ids []string) (map[string]*catalog.Product, error) {
	b.listByIDs++
	if b.err != nil {
		return nil, b.err
	}
	out := make(map[string]*catalog.Product, len(ids))
	for _, id := range ids {
		if p := b.byID[id]; p != nil {
			out[p.ID] = p
		}
	}
	return out, nil
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
	if !v.ProductVisible || v.ProductSlug != "shirt" {
		t.Fatalf("ProductVisible/Slug = %v/%q, want true/shirt", v.ProductVisible, v.ProductSlug)
	}
	batch, err := port.FindVariants(context.Background(), []string{"v1", "missing"})
	if err != nil || batch["v1"] == nil || !batch["v1"].ProductVisible || batch["v1"].ProductSlug != "shirt" {
		t.Fatalf("FindVariants = %+v, err=%v", batch, err)
	}
	if _, ok := batch["missing"]; ok {
		t.Fatal("missing id must be omitted")
	}
}

func TestCatalogPort_FindVariants_BatchOnce(t *testing.T) {
	variants := &batchVariants{stubVariants: stubVariants{byID: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "p1", SKU: "A"},
		"v2": {ID: "v2", ProductID: "p1", SKU: "B"},
	}}}
	products := &batchProducts{stubProducts: stubProducts{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Slug: "shirt", Status: catalog.StatusActive},
	}}}
	port := pluginsdk.NewCatalogPort(products, variants)
	got, err := port.FindVariants(context.Background(), []string{"v1", "v2", "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if variants.findByIDs != 1 {
		t.Fatalf("FindByIDs calls = %d, want 1", variants.findByIDs)
	}
	if products.listByIDs != 1 {
		t.Fatalf("ListByIDs calls = %d, want 1", products.listByIDs)
	}
	if got["v1"].ProductSlug != "shirt" || got["v2"].ProductSlug != "shirt" {
		t.Fatalf("got %+v", got)
	}
}

func TestCatalogPort_FindVariants_NilProductsRepo(t *testing.T) {
	port := pluginsdk.NewCatalogPort(nil, stubVariants{byID: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "p1", SKU: "A"},
	}})
	got, err := port.FindVariants(context.Background(), []string{"v1"})
	if err != nil {
		t.Fatal(err)
	}
	if got["v1"] == nil || got["v1"].ProductVisible || got["v1"].ProductSlug != "" {
		t.Fatalf("want ProductVisible=false with nil products, got %+v", got["v1"])
	}
}

func TestCatalogPort_FindVariants_ProductRepoError(t *testing.T) {
	variants := &batchVariants{stubVariants: stubVariants{byID: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "p1"},
	}}}
	port := pluginsdk.NewCatalogPort(errProducts{err: context.DeadlineExceeded}, variants)
	_, err := port.FindVariants(context.Background(), []string{"v1"})
	if err == nil {
		t.Fatal("expected parent product error")
	}
}

type errProducts struct {
	err error
}

func (e errProducts) FindByID(context.Context, string) (*catalog.Product, error) {
	return nil, e.err
}
func (errProducts) FindBySlug(context.Context, string) (*catalog.Product, error) { return nil, nil }
func (errProducts) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (errProducts) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (errProducts) Create(context.Context, *catalog.Product) error { return nil }
func (errProducts) Update(context.Context, *catalog.Product) error { return nil }

func (e errProducts) ListByIDs(context.Context, []string) (map[string]*catalog.Product, error) {
	return nil, e.err
}

func TestCatalogPort_FindVariants_MissingParentEmptySlug(t *testing.T) {
	port := pluginsdk.NewCatalogPort(stubProducts{byID: map[string]*catalog.Product{}}, stubVariants{byID: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "gone", SKU: "A"},
	}})
	got, err := port.FindVariants(context.Background(), []string{"v1"})
	if err != nil {
		t.Fatal(err)
	}
	if got["v1"] == nil || got["v1"].ProductVisible || got["v1"].ProductSlug != "" {
		t.Fatalf("missing parent must yield ProductVisible=false, got %+v", got["v1"])
	}
	if extapi.ClassifyProductSlug(got["v1"], "shirt") != extapi.ProductSlugUnknown {
		t.Fatal("missing parent must be ProductSlugUnknown")
	}
}

func TestCatalogPort_FindVariants_BlankSlugStillVisible(t *testing.T) {
	port := pluginsdk.NewCatalogPort(stubProducts{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Slug: "", Status: catalog.StatusActive},
	}}, stubVariants{byID: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "p1", SKU: "A"},
	}})
	got, err := port.FindVariants(context.Background(), []string{"v1"})
	if err != nil {
		t.Fatal(err)
	}
	v := got["v1"]
	if v == nil || !v.ProductVisible || v.ProductSlug != "" {
		t.Fatalf("blank slug column: want ProductVisible=true empty slug, got %+v", v)
	}
	if extapi.ClassifyProductSlug(v, "shirt") != extapi.ProductSlugUnknown {
		t.Fatal("blank slug must be ProductSlugUnknown (fail closed)")
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

func TestCatalogSeedPort_EnsureProductValidation(t *testing.T) {
	port := pluginsdk.NewCatalogSeedPort(stubProducts{}, stubVariants{}, nil, nil, nil, nil)
	if _, _, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{Name: "X"}); err == nil {
		t.Fatal("expected empty slug error")
	}
}

type seedProducts struct {
	stubProducts
	bySlug map[string]*catalog.Product
	created []*catalog.Product
}

func (s *seedProducts) FindBySlug(_ context.Context, slug string) (*catalog.Product, error) {
	if s.bySlug == nil {
		return nil, nil
	}
	return s.bySlug[slug], nil
}
func (s *seedProducts) Create(_ context.Context, p *catalog.Product) error {
	s.created = append(s.created, p)
	if s.bySlug == nil {
		s.bySlug = map[string]*catalog.Product{}
	}
	s.bySlug[p.Slug] = p
	return nil
}

func TestCatalogSeedPort_EnsureProductIdempotent(t *testing.T) {
	products := &seedProducts{bySlug: map[string]*catalog.Product{
		"tee": {ID: "p-existing", Slug: "tee", Name: "Tee"},
	}}
	port := pluginsdk.NewCatalogSeedPort(products, stubVariants{}, nil, nil, nil, nil)
	id, created, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee",
	})
	if err != nil || created || id != "p-existing" {
		t.Fatalf("existing: id=%q created=%v err=%v", id, created, err)
	}

	products2 := &seedProducts{}
	port2 := pluginsdk.NewCatalogSeedPort(products2, stubVariants{}, nil, nil, nil, nil)
	id2, created2, err := port2.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Hoodie", Slug: "hoodie", Description: "warm",
	})
	if err != nil || !created2 || id2 == "" {
		t.Fatalf("create: id=%q created=%v err=%v", id2, created2, err)
	}
	if len(products2.created) != 1 || products2.created[0].Slug != "hoodie" {
		t.Fatalf("created = %#v", products2.created)
	}
}

type seedVariants struct {
	stubVariants
	bySKU   map[string]*catalog.Variant
	created []*catalog.Variant
}

func (s *seedVariants) FindBySKU(_ context.Context, sku string) (*catalog.Variant, error) {
	if s.bySKU == nil {
		return nil, nil
	}
	return s.bySKU[sku], nil
}
func (s *seedVariants) Create(_ context.Context, v *catalog.Variant) error {
	s.created = append(s.created, v)
	if s.bySKU == nil {
		s.bySKU = map[string]*catalog.Variant{}
	}
	s.bySKU[v.SKU] = v
	return nil
}

func TestCatalogSeedPort_Import_ResolvesSlugs(t *testing.T) {
	products := &seedProducts{}
	variants := &seedVariants{}
	port := pluginsdk.NewCatalogSeedPort(products, variants, nil, nil, nil, nil)
	res, err := port.Import(context.Background(), pluginsdk.CatalogSeedBatch{
		Products: []pluginsdk.CatalogProductSeed{{Name: "Tee", Slug: "tee"}},
		Variants: []pluginsdk.CatalogVariantSeed{{ProductSlug: "tee", SKU: "TEE-1", Name: "Tee"}},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.ProductIDs["tee"] == "" || res.VariantIDs["TEE-1"] == "" {
		t.Fatalf("result = %#v", res)
	}
	if len(variants.created) != 1 || variants.created[0].ProductID != res.ProductIDs["tee"] {
		t.Fatalf("variant = %#v", variants.created)
	}
}
