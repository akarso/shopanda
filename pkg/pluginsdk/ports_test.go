package pluginsdk_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	domainorder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
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
	if products2.created[0].Status != catalog.StatusDraft {
		t.Fatalf("default status = %q, want draft", products2.created[0].Status)
	}
}

func TestCatalogSeedPort_EnsureProduct_StatusTypeAttributes(t *testing.T) {
	products := &seedProducts{}
	port := pluginsdk.NewCatalogSeedPort(products, stubVariants{}, nil, nil, nil, nil)
	attrs := map[string]any{
		"parent_sku": "TEE-PARENT",
		"meta":       map[string]any{"nested": "v"},
	}
	_, created, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee-attrs", Status: "draft", Type: "virtual", Attributes: attrs,
	})
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	got := products.created[0]
	if got.Status != catalog.StatusDraft {
		t.Fatalf("status = %q, want draft", got.Status)
	}
	if got.Type != catalog.TypeVirtual {
		t.Fatalf("type = %q, want virtual", got.Type)
	}
	if got.Attributes["parent_sku"] != "TEE-PARENT" {
		t.Fatalf("attributes = %#v", got.Attributes)
	}
	attrs["parent_sku"] = "MUTATED"
	attrs["meta"].(map[string]any)["nested"] = "MUTATED"
	if got.Attributes["parent_sku"] != "TEE-PARENT" {
		t.Fatal("seed Attributes map must be deep-cloned")
	}
	nested, _ := got.Attributes["meta"].(map[string]interface{})
	if nested["nested"] != "v" {
		t.Fatalf("nested attribute mutated: %#v", got.Attributes["meta"])
	}

	if _, _, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Bad", Slug: "bad-status", Status: "nope",
	}); err == nil || !strings.Contains(err.Error(), catalog.InvalidStatusMessage(catalog.Status("nope"))) {
		t.Fatalf("invalid status err = %v", err)
	}
	if _, _, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Bad", Slug: "bad-type", Type: "nope",
	}); err == nil || !strings.Contains(err.Error(), catalog.InvalidTypeMessage(catalog.Type("nope"))) {
		t.Fatalf("invalid type err = %v", err)
	}

	productsActive := &seedProducts{}
	portActive := pluginsdk.NewCatalogSeedPort(productsActive, stubVariants{}, nil, nil, nil, nil)
	_, _, err = portActive.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Live", Slug: "live", Status: "active",
	})
	if err != nil {
		t.Fatalf("explicit active: %v", err)
	}
	if productsActive.created[0].Status != catalog.StatusActive {
		t.Fatalf("explicit active status = %q", productsActive.created[0].Status)
	}

	productsArchived := &seedProducts{}
	portArchived := pluginsdk.NewCatalogSeedPort(productsArchived, stubVariants{}, nil, nil, nil, nil)
	_, _, err = portArchived.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Old", Slug: "old", Status: "archived",
	})
	if err != nil {
		t.Fatalf("archived: %v", err)
	}
	if productsArchived.created[0].Status != catalog.StatusArchived {
		t.Fatalf("archived status = %q", productsArchived.created[0].Status)
	}

	productsWS := &seedProducts{}
	portWS := pluginsdk.NewCatalogSeedPort(productsWS, stubVariants{}, nil, nil, nil, nil)
	_, _, err = portWS.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "WS", Slug: "ws", Status: "   ", Type: "   ",
	})
	if err != nil {
		t.Fatalf("whitespace status/type: %v", err)
	}
	if productsWS.created[0].Status != catalog.StatusDraft {
		t.Fatalf("whitespace status = %q, want draft", productsWS.created[0].Status)
	}
	if productsWS.created[0].Type != catalog.TypeSimple {
		t.Fatalf("whitespace type = %q, want simple", productsWS.created[0].Type)
	}
}

func TestCatalogSeedPort_EnsureProduct_StringSliceClone(t *testing.T) {
	products := &seedProducts{}
	port := pluginsdk.NewCatalogSeedPort(products, stubVariants{}, nil, nil, nil, nil)
	tags := []string{"a", "b"}
	_, _, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee-tags", Attributes: map[string]any{"tags": tags},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tags[0] = "MUTATED"
	got := products.created[0].Attributes["tags"]
	// JSON unmarshal → []interface{} of strings
	arr, ok := got.([]interface{})
	if !ok || len(arr) != 2 || arr[0] != "a" {
		t.Fatalf("[]string must be deep-cloned via JSON, got %#v", got)
	}
}

func TestCatalogSeedPort_EnsureProduct_DownloadableActiveRejected(t *testing.T) {
	port := pluginsdk.NewCatalogSeedPort(&seedProducts{}, stubVariants{}, nil, nil, nil, nil)
	_, _, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Ebook", Slug: "ebook", Status: "active", Type: "downloadable",
	})
	if err == nil || !strings.Contains(err.Error(), catalog.DownloadableRequiresFileMessage) {
		t.Fatalf("err = %v, want %q", err, catalog.DownloadableRequiresFileMessage)
	}
}

func TestCatalogSeedPort_EnsureProduct_ExistingMismatch(t *testing.T) {
	products := &seedProducts{bySlug: map[string]*catalog.Product{
		"tee": {
			ID: "p1", Slug: "tee", Name: "Tee",
			Status: catalog.StatusDraft, Type: catalog.TypeSimple,
			Attributes: map[string]interface{}{"parent_sku": "A", "extra": "from-admin"},
		},
	}}
	port := pluginsdk.NewCatalogSeedPort(products, stubVariants{}, nil, nil, nil, nil)

	// Empty seed fields: create-only soft success (no requested Status/Type/Attrs).
	id, created, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee",
	})
	if err != nil || created || id != "p1" {
		t.Fatalf("empty fields: id=%q created=%v err=%v", id, created, err)
	}

	// nil Attributes: do not check (extra stored keys OK).
	if _, _, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee", Status: "draft",
	}); err != nil {
		t.Fatalf("matching status: %v", err)
	}

	// Subset: seed key matches; extra stored key OK.
	if _, _, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee", Attributes: map[string]any{"parent_sku": "A"},
	}); err != nil {
		t.Fatalf("subset attrs: %v", err)
	}

	// {} asserts empty stored map — fails when extras exist.
	_, _, err = port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee", Attributes: map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "create-only") || !strings.Contains(err.Error(), "attributes not empty") {
		t.Fatalf("empty-map assert err = %v", err)
	}

	// Differing Status: error (do not soft-succeed).
	_, _, err = port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee", Status: "active",
	})
	if err == nil || !strings.Contains(err.Error(), "create-only") {
		t.Fatalf("status mismatch err = %v", err)
	}

	_, _, err = port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee", Type: "virtual",
	})
	if err == nil || !strings.Contains(err.Error(), "create-only") {
		t.Fatalf("type mismatch err = %v", err)
	}

	_, _, err = port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee", Attributes: map[string]any{"parent_sku": "B"},
	})
	if err == nil || !strings.Contains(err.Error(), "create-only") || !strings.Contains(err.Error(), `attribute "parent_sku" mismatch`) {
		t.Fatalf("attrs mismatch err = %v", err)
	}

	// Seed key absent from stored → error (cannot add attributes on re-seed).
	_, _, err = port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee", Attributes: map[string]any{"new_key": "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "create-only") || !strings.Contains(err.Error(), `attribute "new_key" mismatch`) {
		t.Fatalf("missing seed key err = %v", err)
	}

	_, _, err = port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "new-bad-attrs", Attributes: map[string]any{"bad": func() {}},
	})
	if err == nil || !strings.Contains(err.Error(), "not JSON-serializable") {
		t.Fatalf("non-JSON attrs err = %v", err)
	}

	// {} vs empty stored: ok.
	empty := &seedProducts{bySlug: map[string]*catalog.Product{
		"bare": {ID: "p2", Slug: "bare", Status: catalog.StatusDraft, Type: catalog.TypeSimple, Attributes: map[string]interface{}{}},
	}}
	portEmpty := pluginsdk.NewCatalogSeedPort(empty, stubVariants{}, nil, nil, nil, nil)
	if _, _, err := portEmpty.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Bare", Slug: "bare", Attributes: map[string]any{},
	}); err != nil {
		t.Fatalf("empty assert on empty stored: %v", err)
	}
}

func TestCatalogSeedPort_EnsureVariant_Attributes(t *testing.T) {
	variants := &seedVariants{}
	port := pluginsdk.NewCatalogSeedPort(&seedProducts{}, variants, nil, nil, nil, nil)
	tags := []string{"a", "b"}
	attrs := map[string]any{"size": "M", "tags": tags}
	_, created, err := port.EnsureVariant(context.Background(), pluginsdk.CatalogVariantSeed{
		ProductID: "p1", SKU: "TEE-M", Name: "Tee M", Attributes: attrs,
	})
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	got := variants.created[0]
	if got.Attributes["size"] != "M" {
		t.Fatalf("attributes = %#v", got.Attributes)
	}
	attrs["size"] = "L"
	tags[0] = "Z"
	if got.Attributes["size"] != "M" {
		t.Fatal("seed Attributes map must be deep-cloned")
	}
	clonedTags, _ := got.Attributes["tags"].([]interface{})
	if clonedTags[0] != "a" {
		t.Fatalf("[]string must be deep-cloned, got %#v", got.Attributes["tags"])
	}

	// Existing SKU + different attrs → error.
	_, _, err = port.EnsureVariant(context.Background(), pluginsdk.CatalogVariantSeed{
		ProductID: "p1", SKU: "TEE-M", Name: "Tee M", Attributes: map[string]any{"size": "L"},
	})
	if err == nil || !strings.Contains(err.Error(), "create-only") {
		t.Fatalf("variant attrs mismatch err = %v", err)
	}
	// Subset match (only size): soft success even if stored has tags.
	id, created, err := port.EnsureVariant(context.Background(), pluginsdk.CatalogVariantSeed{
		ProductID: "p1", SKU: "TEE-M", Attributes: map[string]any{"size": "M"},
	})
	if err != nil || created || id == "" {
		t.Fatalf("subset attrs: id=%q created=%v err=%v", id, created, err)
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

func TestCatalogSeedPort_Import_StatusTypeAttributes(t *testing.T) {
	products := &seedProducts{}
	variants := &seedVariants{}
	port := pluginsdk.NewCatalogSeedPort(products, variants, nil, nil, nil, nil)
	res, err := port.Import(context.Background(), pluginsdk.CatalogSeedBatch{
		Products: []pluginsdk.CatalogProductSeed{{
			Name: "Tee", Slug: "tee", Status: "draft", Type: "virtual",
			Attributes: map[string]any{"parent_sku": "TEE-PARENT"},
		}},
		Variants: []pluginsdk.CatalogVariantSeed{{
			ProductSlug: "tee", SKU: "TEE-M", Name: "M",
			Attributes: map[string]any{"size": "M"},
		}},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.ProductIDs["tee"] == "" || res.VariantIDs["TEE-M"] == "" {
		t.Fatalf("result = %#v", res)
	}
	p := products.created[0]
	if p.Status != catalog.StatusDraft || p.Type != catalog.TypeVirtual || p.Attributes["parent_sku"] != "TEE-PARENT" {
		t.Fatalf("product = %#v", p)
	}
	v := variants.created[0]
	if v.Attributes["size"] != "M" {
		t.Fatalf("variant = %#v", v)
	}
}

func TestCatalogSeedPort_Import_MismatchFailsBatch(t *testing.T) {
	products := &seedProducts{bySlug: map[string]*catalog.Product{
		"tee": {
			ID: "p1", Slug: "tee", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
			Attributes: map[string]interface{}{},
		},
	}}
	port := pluginsdk.NewCatalogSeedPort(products, &seedVariants{}, nil, nil, nil, nil)
	_, err := port.Import(context.Background(), pluginsdk.CatalogSeedBatch{
		Products: []pluginsdk.CatalogProductSeed{{
			Name: "Tee", Slug: "tee", Status: "active",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "create-only") {
		t.Fatalf("Import mismatch err = %v", err)
	}
}

type conflictProducts struct {
	seedProducts
	conflictOnce bool
}

func (s *conflictProducts) FindBySlug(_ context.Context, slug string) (*catalog.Product, error) {
	// First Ensure lookup: miss so Create runs; after conflict Create stores the row.
	if s.bySlug == nil {
		return nil, nil
	}
	return s.bySlug[slug], nil
}

func (s *conflictProducts) Create(_ context.Context, p *catalog.Product) error {
	if !s.conflictOnce {
		s.conflictOnce = true
		if s.bySlug == nil {
			s.bySlug = map[string]*catalog.Product{}
		}
		// Simulate a concurrent create winning the race.
		existing := *p
		existing.ID = "p-raced"
		existing.Status = catalog.StatusDraft
		existing.Type = catalog.TypeSimple
		existing.Attributes = map[string]interface{}{}
		s.bySlug[p.Slug] = &existing
		return apperror.Conflict("slug taken")
	}
	return s.seedProducts.Create(context.Background(), p)
}

func TestCatalogSeedPort_EnsureProduct_ConflictRereadMatch(t *testing.T) {
	products := &conflictProducts{}
	port := pluginsdk.NewCatalogSeedPort(products, stubVariants{}, nil, nil, nil, nil)

	// Seed Status matches the raced row → soft success after conflict re-read.
	id, created, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee-race", Status: "draft",
	})
	if err != nil || created || id != "p-raced" {
		t.Fatalf("match after conflict: id=%q created=%v err=%v", id, created, err)
	}

	// Fresh port: conflict again, but seed Status disagrees with raced row.
	products2 := &conflictProducts{}
	port2 := pluginsdk.NewCatalogSeedPort(products2, stubVariants{}, nil, nil, nil, nil)
	_, _, err = port2.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Tee", Slug: "tee-race2", Status: "active",
	})
	if err == nil || !strings.Contains(err.Error(), "create-only") {
		t.Fatalf("mismatch after conflict err = %v", err)
	}
}

type seedPrices struct {
	byKey map[string]*pricing.Price
}

func priceKey(variantID, currency, storeID string) string {
	return variantID + "|" + currency + "|" + storeID
}

func (s *seedPrices) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	if s.byKey == nil {
		return nil, nil
	}
	return s.byKey[priceKey(variantID, currency, storeID)], nil
}
func (s *seedPrices) FindByVariantsCurrencyAndStore(context.Context, []string, string, string) (map[string]*pricing.Price, error) {
	return nil, nil
}
func (s *seedPrices) ListByVariantID(context.Context, string) ([]pricing.Price, error) { return nil, nil }
func (s *seedPrices) List(context.Context, int, int) ([]pricing.Price, error)          { return nil, nil }
func (s *seedPrices) Upsert(_ context.Context, p *pricing.Price) error {
	if s.byKey == nil {
		s.byKey = map[string]*pricing.Price{}
	}
	cp := *p
	s.byKey[priceKey(p.VariantID, p.Amount.Currency(), p.StoreID)] = &cp
	return nil
}

type seedHistory struct {
	snaps []*pricing.PriceSnapshot
}

func (s *seedHistory) Record(_ context.Context, snap *pricing.PriceSnapshot) error {
	cp := *snap
	s.snaps = append(s.snaps, &cp)
	return nil
}
func (s *seedHistory) LowestSince(context.Context, string, string, string, time.Time) (*pricing.PriceSnapshot, error) {
	return nil, nil
}
func (s *seedHistory) LowestSinceByVariants(context.Context, []string, string, string, time.Time) (map[string]*pricing.PriceSnapshot, error) {
	return nil, nil
}

func TestCatalogSeedPort_EnsurePrice_CreateOnlyAndUpdate(t *testing.T) {
	prices := &seedPrices{}
	history := &seedHistory{}
	port := pluginsdk.NewCatalogSeedPort(&seedProducts{}, stubVariants{}, prices, history, nil, nil)

	written, err := port.EnsurePrice(context.Background(), pluginsdk.CatalogPriceSeed{
		VariantID: "v1", Amount: 1000, Currency: "PLN",
	})
	if err != nil || !written {
		t.Fatalf("create: written=%v err=%v", written, err)
	}
	if len(history.snaps) != 1 || history.snaps[0].Amount.Amount() != 1000 {
		t.Fatalf("history after create = %#v", history.snaps)
	}

	// Create-only: existing row unchanged, no new history.
	written, err = port.EnsurePrice(context.Background(), pluginsdk.CatalogPriceSeed{
		VariantID: "v1", Amount: 2500, Currency: "PLN",
	})
	if err != nil || written {
		t.Fatalf("create-only hit: written=%v err=%v", written, err)
	}
	got := prices.byKey[priceKey("v1", "PLN", "")]
	if got.Amount.Amount() != 1000 {
		t.Fatalf("create-only mutated amount to %d", got.Amount.Amount())
	}
	if len(history.snaps) != 1 {
		t.Fatalf("create-only wrote history: %#v", history.snaps)
	}

	// Update=true, same amount: no-op.
	written, err = port.EnsurePrice(context.Background(), pluginsdk.CatalogPriceSeed{
		VariantID: "v1", Amount: 1000, Currency: "PLN", Update: true,
	})
	if err != nil || written {
		t.Fatalf("update same: written=%v err=%v", written, err)
	}
	if len(history.snaps) != 1 {
		t.Fatalf("same-amount history = %#v", history.snaps)
	}

	// Update=true, new amount: write + history; keep price id.
	prevID := got.ID
	written, err = port.EnsurePrice(context.Background(), pluginsdk.CatalogPriceSeed{
		VariantID: "v1", Amount: 2500, Currency: "PLN", Update: true,
	})
	if err != nil || !written {
		t.Fatalf("update: written=%v err=%v", written, err)
	}
	got = prices.byKey[priceKey("v1", "PLN", "")]
	if got.Amount.Amount() != 2500 || got.ID != prevID {
		t.Fatalf("updated price = %#v, want id %q amount 2500", got, prevID)
	}
	if len(history.snaps) != 2 || history.snaps[1].Amount.Amount() != 2500 {
		t.Fatalf("history after update = %#v", history.snaps)
	}
}

func TestCatalogSeedPort_Import_PriceUpdate(t *testing.T) {
	prices := &seedPrices{}
	history := &seedHistory{}
	money, err := shared.NewMoney(1000, "PLN")
	if err != nil {
		t.Fatal(err)
	}
	prices.byKey = map[string]*pricing.Price{
		priceKey("v1", "PLN", ""): {ID: "p-existing", VariantID: "v1", Amount: money},
	}
	port := pluginsdk.NewCatalogSeedPort(&seedProducts{}, stubVariants{}, prices, history, nil, nil)
	_, err = port.Import(context.Background(), pluginsdk.CatalogSeedBatch{
		Prices: []pluginsdk.CatalogPriceSeed{{
			VariantID: "v1", Amount: 3333, Currency: "PLN", Update: true,
		}},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	got := prices.byKey[priceKey("v1", "PLN", "")]
	if got.Amount.Amount() != 3333 || got.ID != "p-existing" {
		t.Fatalf("import update = %#v", got)
	}
	if len(history.snaps) != 1 {
		t.Fatalf("import history = %#v", history.snaps)
	}
}

func TestCatalogSeedPort_EnsurePrice_CreateOnlyIgnoresInvalidCurrency(t *testing.T) {
	money := shared.MustNewMoney(1000, "PLN")
	prices := &seedPrices{byKey: map[string]*pricing.Price{
		// Lookup key uses the seed currency string; seed below uses "PLN" for Find
		// then we also assert a typo currency soft-no-ops only when Find hits.
		priceKey("v1", "PLN", ""): {ID: "p1", VariantID: "v1", Amount: money},
	}}
	history := &seedHistory{}
	port := pluginsdk.NewCatalogSeedPort(&seedProducts{}, stubVariants{}, prices, history, nil, nil)

	// Existing hit + Update=false: soft no-op even with a nonsense amount
	// (currency must still match the lookup key to find the row).
	written, err := port.EnsurePrice(context.Background(), pluginsdk.CatalogPriceSeed{
		VariantID: "v1", Amount: -999, Currency: "PLN",
	})
	if err != nil || written {
		t.Fatalf("create-only hit: written=%v err=%v", written, err)
	}
	if prices.byKey[priceKey("v1", "PLN", "")].Amount.Amount() != 1000 {
		t.Fatal("create-only mutated amount")
	}

	// Invalid currency that does not match any row → create path validates money.
	_, err = port.EnsurePrice(context.Background(), pluginsdk.CatalogPriceSeed{
		VariantID: "v1", Amount: 1000, Currency: "Pln",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid money") {
		t.Fatalf("miss + bad currency err = %v", err)
	}
}

func TestCatalogSeedPort_EnsurePrice_UpdateCreateMissAndStoreScoped(t *testing.T) {
	prices := &seedPrices{}
	history := &seedHistory{}
	port := pluginsdk.NewCatalogSeedPort(&seedProducts{}, stubVariants{}, prices, history, nil, nil)

	// Update=true with no existing row → create.
	written, err := port.EnsurePrice(context.Background(), pluginsdk.CatalogPriceSeed{
		VariantID: "v1", Amount: 1000, Currency: "PLN", StoreID: "store-a", Update: true,
	})
	if err != nil || !written {
		t.Fatalf("create-miss: written=%v err=%v", written, err)
	}
	got := prices.byKey[priceKey("v1", "PLN", "store-a")]
	if got == nil || got.StoreID != "store-a" || got.Amount.Amount() != 1000 {
		t.Fatalf("store-scoped create = %#v", got)
	}
	prevID := got.ID

	written, err = port.EnsurePrice(context.Background(), pluginsdk.CatalogPriceSeed{
		VariantID: "v1", Amount: 2000, Currency: "PLN", StoreID: "store-a", Update: true,
	})
	if err != nil || !written {
		t.Fatalf("store-scoped update: written=%v err=%v", written, err)
	}
	got = prices.byKey[priceKey("v1", "PLN", "store-a")]
	if got.ID != prevID || got.Amount.Amount() != 2000 {
		t.Fatalf("store-scoped update = %#v, want id %q", got, prevID)
	}
	// Global key untouched.
	if prices.byKey[priceKey("v1", "PLN", "")] != nil {
		t.Fatal("unexpected global price row")
	}
}

func TestCatalogSeedPort_Import_CreateOnlyLeavesAmount(t *testing.T) {
	money := shared.MustNewMoney(1000, "PLN")
	prices := &seedPrices{byKey: map[string]*pricing.Price{
		priceKey("v1", "PLN", ""): {ID: "p1", VariantID: "v1", Amount: money},
	}}
	history := &seedHistory{}
	port := pluginsdk.NewCatalogSeedPort(&seedProducts{}, stubVariants{}, prices, history, nil, nil)
	_, err := port.Import(context.Background(), pluginsdk.CatalogSeedBatch{
		Prices: []pluginsdk.CatalogPriceSeed{{
			VariantID: "v1", Amount: 9999, Currency: "PLN", // Update=false
		}},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if prices.byKey[priceKey("v1", "PLN", "")].Amount.Amount() != 1000 {
		t.Fatal("Import create-only changed amount")
	}
	if len(history.snaps) != 0 {
		t.Fatalf("unexpected history: %#v", history.snaps)
	}
}

func TestCatalogSeedPort_EnsureProduct_EmptyStatusOnActiveIsNoOp(t *testing.T) {
	// Documented contract: empty Status on an existing active product is soft
	// success (create-only does not check unset Status).
	products := &seedProducts{bySlug: map[string]*catalog.Product{
		"live": {ID: "p1", Slug: "live", Status: catalog.StatusActive, Type: catalog.TypeSimple},
	}}
	port := pluginsdk.NewCatalogSeedPort(products, stubVariants{}, nil, nil, nil, nil)
	id, created, err := port.EnsureProduct(context.Background(), pluginsdk.CatalogProductSeed{
		Name: "Live", Slug: "live",
	})
	if err != nil || created || id != "p1" {
		t.Fatalf("empty status on active: id=%q created=%v err=%v", id, created, err)
	}
}
