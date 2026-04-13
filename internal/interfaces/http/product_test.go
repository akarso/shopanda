package http_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/translation"
	"github.com/akarso/shopanda/internal/platform/apperror"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- mock repository ---

type mockProductRepo struct {
	findByIDFn func(ctx context.Context, id string) (*catalog.Product, error)
	listFn     func(ctx context.Context, offset, limit int) ([]catalog.Product, error)
}

func (m *mockProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	return m.findByIDFn(ctx, id)
}

func (m *mockProductRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	return nil, nil
}

func (m *mockProductRepo) List(ctx context.Context, offset, limit int) ([]catalog.Product, error) {
	return m.listFn(ctx, offset, limit)
}

func (m *mockProductRepo) Create(ctx context.Context, p *catalog.Product) error {
	return nil
}

func (m *mockProductRepo) Update(ctx context.Context, p *catalog.Product) error {
	return nil
}
func (m *mockProductRepo) FindByCategoryID(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) WithTx(_ *sql.Tx) catalog.ProductRepository { return m }

// --- mock step ---

type addBlockStep struct {
	name string
	typ  string
}

func (s addBlockStep) Name() string { return s.name }
func (s addBlockStep) Apply(ctx *composition.ProductContext) error {
	ctx.Blocks = append(ctx.Blocks, composition.Block{Type: s.typ, Data: map[string]interface{}{"from": s.name}})
	return nil
}

type addListingBlockStep struct {
	name string
	typ  string
}

func (s addListingBlockStep) Name() string { return s.name }
func (s addListingBlockStep) Apply(ctx *composition.ListingContext) error {
	ctx.Blocks = append(ctx.Blocks, composition.Block{Type: s.typ, Data: map[string]interface{}{"from": s.name}})
	return nil
}

type failStep struct{}

func (failStep) Name() string                                { return "fail" }
func (failStep) Apply(ctx *composition.ProductContext) error { return errors.New("boom") }

// --- helpers ---

func newRouter(h *shophttp.ProductHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/products", h.List())
	mux.HandleFunc("GET /api/v1/products/{id}", h.Get())
	return mux
}

func parseBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return body
}

// --- tests ---

func TestProductHandler_List_OK(t *testing.T) {
	repo := &mockProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			return []catalog.Product{
				{ID: "p1", Name: "Widget"},
				{ID: "p2", Name: "Gadget"},
			}, nil
		},
	}
	plp := composition.NewPipeline[composition.ListingContext]()
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products?offset=0&limit=10", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := parseBody(t, rec)
	data := body["data"].(map[string]interface{})
	products := data["products"].([]interface{})
	if len(products) != 2 {
		t.Errorf("products len = %d, want 2", len(products))
	}
}

func TestProductHandler_List_DefaultPagination(t *testing.T) {
	var capturedOffset, capturedLimit int
	repo := &mockProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			capturedOffset = offset
			capturedLimit = limit
			return nil, nil
		},
	}
	plp := composition.NewPipeline[composition.ListingContext]()
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturedOffset != 0 {
		t.Errorf("offset = %d, want 0", capturedOffset)
	}
	if capturedLimit != 20 {
		t.Errorf("limit = %d, want 20", capturedLimit)
	}
}

func TestProductHandler_List_InvalidOffset(t *testing.T) {
	repo := &mockProductRepo{}
	plp := composition.NewPipeline[composition.ListingContext]()
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products?offset=-1", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductHandler_List_InvalidLimit(t *testing.T) {
	repo := &mockProductRepo{}
	plp := composition.NewPipeline[composition.ListingContext]()
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products?limit=0", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductHandler_List_WithPipeline(t *testing.T) {
	repo := &mockProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			return []catalog.Product{{ID: "p1", Name: "W"}}, nil
		},
	}
	plp := composition.NewPipeline[composition.ListingContext]()
	plp.AddStep(addListingBlockStep{name: "grid", typ: "product_grid"})
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := parseBody(t, rec)
	data := body["data"].(map[string]interface{})
	blocks := data["blocks"].([]interface{})
	if len(blocks) != 1 {
		t.Errorf("blocks len = %d, want 1", len(blocks))
	}
}

func TestProductHandler_List_RepoError(t *testing.T) {
	repo := &mockProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			return nil, apperror.Internal("db down")
		},
	}
	plp := composition.NewPipeline[composition.ListingContext]()
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestProductHandler_Get_OK(t *testing.T) {
	repo := &mockProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget"}, nil
		},
	}
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/p1", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := parseBody(t, rec)
	data := body["data"].(map[string]interface{})
	product := data["product"].(map[string]interface{})
	if product["ID"] != "p1" {
		t.Errorf("product ID = %v, want p1", product["ID"])
	}
}

func TestProductHandler_Get_NotFound(t *testing.T) {
	repo := &mockProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return nil, nil
		},
	}
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/missing", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestProductHandler_Get_RepoError(t *testing.T) {
	repo := &mockProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return nil, apperror.Internal("db down")
		},
	}
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/p1", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestProductHandler_Get_WithPipeline(t *testing.T) {
	repo := &mockProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget"}, nil
		},
	}
	pdp := composition.NewPipeline[composition.ProductContext]()
	pdp.AddStep(addBlockStep{name: "hero", typ: "hero_banner"})
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/p1", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := parseBody(t, rec)
	data := body["data"].(map[string]interface{})
	blocks := data["blocks"].([]interface{})
	if len(blocks) != 1 {
		t.Errorf("blocks len = %d, want 1", len(blocks))
	}
}

func TestProductHandler_Get_PipelineError(t *testing.T) {
	repo := &mockProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget"}, nil
		},
	}
	pdp := composition.NewPipeline[composition.ProductContext]()
	pdp.AddStep(failStep{})
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/p1", nil)
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestProductHandler_Get_WithContentTranslation(t *testing.T) {
	repo := &mockProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Description: "A nice widget"}, nil
		},
	}
	ct := translation.NewContentTranslator(&mockContentTranslationRepo{
		findByEntityAndLanguageFn: func(_ context.Context, entityID, lang string) ([]translation.ContentTranslation, error) {
			if entityID == "p1" && lang == "de" {
				return []translation.ContentTranslation{
					{EntityID: entityID, Language: lang, Field: "name", Value: "Ding"},
					{EntityID: entityID, Language: lang, Field: "description", Value: "Ein schönes Ding"},
				}, nil
			}
			return []translation.ContentTranslation{}, nil
		},
	}, nil)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewProductHandler(repo, pdp, plp, ct)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/p1", nil)
	req = req.WithContext(translation.WithLanguage(req.Context(), "de"))
	newRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := parseBody(t, rec)
	data := body["data"].(map[string]interface{})
	product := data["product"].(map[string]interface{})
	if product["Name"] != "Ding" {
		t.Errorf("Name = %v, want Ding", product["Name"])
	}
	if product["Description"] != "Ein schönes Ding" {
		t.Errorf("Description = %v, want Ein schönes Ding", product["Description"])
	}
}
