package storefront_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	reviewsApp "github.com/akarso/shopanda/internal/application/reviews"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainReview "github.com/akarso/shopanda/internal/domain/review"
	"github.com/akarso/shopanda/internal/domain/search"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
)

type stubReviewRepo struct{}

type recordingReviewRepo struct {
	stubReviewRepo
	lastProductID string
	list          []domainReview.Review
}

func (r *recordingReviewRepo) ListApprovedByProduct(_ context.Context, productID string, _, _ int) ([]domainReview.Review, error) {
	r.lastProductID = productID
	return r.list, nil
}

func (stubReviewRepo) Save(context.Context, *domainReview.Review) error { return nil }
func (stubReviewRepo) FindByID(context.Context, string) (*domainReview.Review, error) {
	return nil, nil
}
func (stubReviewRepo) FindByProductAndCustomer(context.Context, string, string) (*domainReview.Review, error) {
	return nil, nil
}
func (stubReviewRepo) ListApprovedByProduct(context.Context, string, int, int) ([]domainReview.Review, error) {
	return nil, nil
}
func (stubReviewRepo) SummaryByProduct(context.Context, string) (domainReview.ProductSummary, error) {
	return domainReview.ProductSummary{}, nil
}
func (stubReviewRepo) List(context.Context, domainReview.Status, int, int) ([]domainReview.Review, error) {
	return nil, nil
}
func (stubReviewRepo) Update(context.Context, *domainReview.Review, domainReview.Status) error {
	return nil
}

type enforceFacts map[string]catalog.VisibilitySnapshot

func (m enforceFacts) LoadMany(_ context.Context, ids []string) (map[string]catalog.VisibilitySnapshot, error) {
	out := make(map[string]catalog.VisibilitySnapshot, len(ids))
	for _, id := range ids {
		if s, ok := m[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

func enforceSnap(modes catalog.VisibilityAxes) catalog.VisibilitySnapshot {
	return catalog.VisibilitySnapshot{
		Status: catalog.StatusActive,
		Modes:  modes,
		Inputs: catalog.VisibilityInputs{
			QuantityPositive:   true,
			HasPrice:           true,
			HasStoreAssignment: true,
		},
	}
}

func TestProductHandler_Get_HiddenIndividually(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	modes.Individually = catalog.VisibilityModeHidden
	repo := &mockProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Secret", Status: catalog.StatusActive, VisibilityModes: modes}, nil
		},
	}
	h := storefront.NewProductHandler(repo, composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), nil).
		WithVisibilityFacts(enforceFacts{"p1": enforceSnap(modes)})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/p1", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestProductHandler_Get_VisibleIndividually(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	modes.Individually = catalog.VisibilityModeVisible
	repo := &mockProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Shown", Status: catalog.StatusActive, VisibilityModes: modes}, nil
		},
	}
	h := storefront.NewProductHandler(repo, composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), nil).
		WithVisibilityFacts(enforceFacts{"p1": enforceSnap(modes)})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/p1", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProductHandler_List_FiltersHiddenCatalog(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Catalog = catalog.VisibilityModeHidden
	repo := &mockProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			return []catalog.Product{
				{ID: "h1", Name: "Hidden", Status: catalog.StatusActive, VisibilityModes: hidden},
				{ID: "v1", Name: "Visible", Status: catalog.StatusActive, VisibilityModes: modes},
			}, nil
		},
	}
	facts := enforceFacts{
		"h1": enforceSnap(hidden),
		"v1": enforceSnap(modes),
	}
	h := storefront.NewProductHandler(repo, composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), nil).
		WithVisibilityFacts(facts)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products?limit=10", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := parseBody(t, rec)
	data := body["data"].(map[string]interface{})
	products := data["products"].([]interface{})
	if len(products) != 1 {
		t.Fatalf("products len = %d, want 1", len(products))
	}
	p := products[0].(map[string]interface{})
	id, _ := p["ID"].(string)
	if id != "v1" {
		t.Fatalf("id = %v, want v1 (product=%v)", id, p)
	}
}

func TestSearchHandler_FiltersHiddenSearch(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Search = catalog.VisibilityModeHidden
	engine := &mockSearchEngine{
		searchFn: func(_ context.Context, _ search.SearchQuery) (search.SearchResult, error) {
			return search.SearchResult{
				Products: []search.Product{
					{ID: "h1", Name: "Hidden"},
					{ID: "v1", Name: "Visible"},
				},
				Total: 2,
			}, nil
		},
	}
	h := storefront.NewSearchHandler(engine).WithVisibilityFacts(enforceFacts{
		"h1": enforceSnap(hidden),
		"v1": enforceSnap(modes),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?q=x", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := parseSearchBody(t, rec)
	data := body["data"].(map[string]interface{})
	products := data["products"].([]interface{})
	if len(products) != 1 {
		t.Fatalf("products len = %d, want 1 (total may still be 2)", len(products))
	}
}

func TestSuggestHandler_FiltersHiddenSearch(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Search = catalog.VisibilityModeHidden
	engine := &mockSearchEngine{
		suggestFn: func(_ context.Context, _ string, _ int) ([]search.Suggestion, error) {
			return []search.Suggestion{
				{Text: "Hidden", Type: "product", URL: "/products/h", ProductID: "h1"},
				{Text: "Visible", Type: "product", URL: "/products/v", ProductID: "v1"},
			}, nil
		},
	}
	h := storefront.NewSearchHandler(engine).WithVisibilityFacts(enforceFacts{
		"h1": enforceSnap(hidden),
		"v1": enforceSnap(modes),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search/suggest?q=v", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var wrap map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrap); err != nil {
		t.Fatal(err)
	}
	data := wrap["data"].(map[string]interface{})
	suggestions := data["suggestions"].([]interface{})
	if len(suggestions) != 1 {
		t.Fatalf("suggestions len = %d, want 1", len(suggestions))
	}
}

func TestProductHandler_List_ResumeAfterCursor(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Catalog = catalog.VisibilityModeHidden
	all := make([]catalog.Product, 0, 40)
	facts := enforceFacts{}
	for i := 0; i < 30; i++ {
		id := "h" + itoaEnforce(i)
		all = append(all, catalog.Product{ID: id, Status: catalog.StatusActive, VisibilityModes: hidden})
		facts[id] = enforceSnap(hidden)
	}
	for i := 0; i < 3; i++ {
		id := "v" + itoaEnforce(i)
		all = append(all, catalog.Product{ID: id, Name: id, Status: catalog.StatusActive, VisibilityModes: modes})
		facts[id] = enforceSnap(modes)
	}
	repo := &mockProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			if filter.Offset >= len(all) {
				return nil, nil
			}
			end := filter.Offset + filter.Limit
			if end > len(all) {
				end = len(all)
			}
			return all[filter.Offset:end], nil
		},
	}
	h := storefront.NewProductHandler(repo, composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), nil).
		WithVisibilityFacts(facts)

	after := catalog.EncodeListCursor(30, 0)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products?limit=10&after="+after, nil)
	newRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := parseBody(t, rec)
	data := body["data"].(map[string]interface{})
	products := data["products"].([]interface{})
	if len(products) != 3 {
		t.Fatalf("after cursor products len=%d want 3", len(products))
	}
	meta := data["meta"].(map[string]interface{})
	if meta["truncated"] != false {
		t.Fatalf("truncated = %v", meta["truncated"])
	}
}

func TestProductHandler_List_AfterWithEnforceOff(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	all := make([]catalog.Product, 0, 20)
	for i := 0; i < 20; i++ {
		all = append(all, catalog.Product{ID: "p" + itoaEnforce(i), Status: catalog.StatusActive, VisibilityModes: modes})
	}
	repo := &mockProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			if filter.Offset >= len(all) {
				return nil, nil
			}
			end := filter.Offset + filter.Limit
			if end > len(all) {
				end = len(all)
			}
			return all[filter.Offset:end], nil
		},
	}
	h := storefront.NewProductHandler(repo, composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), nil)
	after := catalog.EncodeListCursor(5, 0)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products?limit=3&after="+after, nil)
	newRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := parseBody(t, rec)
	products := body["data"].(map[string]interface{})["products"].([]interface{})
	if len(products) != 3 {
		t.Fatalf("len=%d", len(products))
	}
	p0 := products[0].(map[string]interface{})
	id, _ := p0["ID"].(string)
	if id != "p5" {
		t.Fatalf("id=%q want p5 (after honoured with enforce off)", id)
	}
}

func TestProductHandler_List_AfterIgnoresBadOffset(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	all := make([]catalog.Product, 0, 10)
	for i := 0; i < 10; i++ {
		all = append(all, catalog.Product{ID: "p" + itoaEnforce(i), Status: catalog.StatusActive, VisibilityModes: modes})
	}
	repo := &mockProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			if filter.Offset >= len(all) {
				return nil, nil
			}
			end := filter.Offset + filter.Limit
			if end > len(all) {
				end = len(all)
			}
			return all[filter.Offset:end], nil
		},
	}
	h := storefront.NewProductHandler(repo, composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), nil)
	after := catalog.EncodeListCursor(4, 0)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products?after="+after+"&offset=-1&limit=2", nil)
	newRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s (after must ignore invalid offset)", rec.Code, rec.Body.String())
	}
	products := parseBody(t, rec)["data"].(map[string]interface{})["products"].([]interface{})
	if len(products) != 2 {
		t.Fatalf("len=%d", len(products))
	}
}

func TestCategoryHandler_Products_InvalidAfter(t *testing.T) {
	catRepo := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			return &catalog.Category{ID: id, Name: "Cat", Slug: "cat"}, nil
		},
	}
	h := storefront.NewCategoryHandler(catRepo, &mockProductRepo{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/categories/{id}/products", h.Products())

	overCap := catalog.EncodeListCursor(catalog.MaxListCursorOffset+1, 0)
	for _, after := range []string{"not-a-cursor", "-1", "42", overCap} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/categories/c1/products?after="+after, nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("after=%q status = %d, want 422", after, rec.Code)
		}
	}
}

func itoaEnforce(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestVariantHandler_List_HiddenParent(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	modes.Individually = catalog.VisibilityModeHidden
	prod := &catalog.Product{ID: "prod-1", Name: "Hidden", Status: catalog.StatusActive, VisibilityModes: modes}
	prodRepo := &mockVariantProductRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
		if id == prod.ID {
			return prod, nil
		}
		return nil, nil
	}}
	h := storefront.NewVariantHandler(prodRepo, &mockVariantRepo{}, testVariantBus()).
		WithVisibilityFacts(enforceFacts{prod.ID: enforceSnap(modes)})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/prod-1/variants", nil)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestStorefrontHandler_Product_HiddenIndividually(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	modes.Individually = catalog.VisibilityModeHidden
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: "p1", Name: "Secret", Slug: slug, Status: catalog.StatusActive, VisibilityModes: modes,
			}, nil
		},
	}
	engine := createTestTheme(t)
	h := storefront.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), newStorefrontSearchMock()).
		WithVisibilityFacts(enforceFacts{"p1": enforceSnap(modes)})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/secret", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestReviewHandler_List_BySlug(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	modes.Individually = catalog.VisibilityModeVisible
	prod := &catalog.Product{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive, VisibilityModes: modes}
	prodRepo := &mockProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			if id == prod.ID {
				return prod, nil
			}
			return nil, nil
		},
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return prod, nil
			}
			return nil, nil
		},
	}
	repo := &recordingReviewRepo{
		list: []domainReview.Review{{ID: "r1", ProductID: "p1", Title: "ok"}},
	}
	svc := reviewsApp.NewService(repo, prodRepo)
	h := storefront.NewReviewHandler(svc).WithVisibilityGate(prodRepo, enforceFacts{prod.ID: enforceSnap(modes)})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/products/{id}/reviews", h.List())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/widget/reviews", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if repo.lastProductID != "p1" {
		t.Fatalf("listed reviews for %q, want p1", repo.lastProductID)
	}
}

func TestReviewHandler_List_HiddenParent(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	modes.Individually = catalog.VisibilityModeHidden
	prod := &catalog.Product{ID: "p1", Name: "Hidden", Status: catalog.StatusActive, VisibilityModes: modes}
	prodRepo := &mockProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			if id == prod.ID {
				return prod, nil
			}
			return nil, nil
		},
	}
	svc := reviewsApp.NewService(stubReviewRepo{}, prodRepo)
	h := storefront.NewReviewHandler(svc).WithVisibilityGate(prodRepo, enforceFacts{prod.ID: enforceSnap(modes)})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/products/{id}/reviews", h.List())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/p1/reviews", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCategoryHandler_Products_MetaAlwaysPresent(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	catRepo := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			return &catalog.Category{ID: id, Name: "Cat", Slug: "cat"}, nil
		},
	}
	prodRepo := &mockProductRepo{
		findByCategoryFn: func(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
			return []catalog.Product{{ID: "v1", Status: catalog.StatusActive, VisibilityModes: modes}}, nil
		},
	}
	h := storefront.NewCategoryHandler(catRepo, prodRepo).WithVisibilityFacts(enforceFacts{
		"v1": enforceSnap(modes),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/categories/{id}/products", h.Products())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/categories/c1/products?limit=20", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := parseBody(t, rec)
	data := body["data"].(map[string]interface{})
	meta, ok := data["meta"].(map[string]interface{})
	if !ok {
		t.Fatal("meta must always be present on category products")
	}
	if _, ok := meta["truncated"]; !ok {
		t.Fatal("meta.truncated missing")
	}
	if _, ok := meta["has_more"]; !ok {
		t.Fatal("meta.has_more missing")
	}
	if _, ok := meta["depth_limited"]; !ok {
		t.Fatal("meta.depth_limited missing")
	}
}
