package http_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- mock repos for variant tests ---

type mockVariantProductRepo struct {
	findByIDFn func(ctx context.Context, id string) (*catalog.Product, error)
}

func (m *mockVariantProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}
func (m *mockVariantProductRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	return nil, nil
}
func (m *mockVariantProductRepo) List(ctx context.Context, offset, limit int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockVariantProductRepo) Create(ctx context.Context, p *catalog.Product) error {
	return nil
}
func (m *mockVariantProductRepo) Update(ctx context.Context, p *catalog.Product) error {
	return nil
}
func (m *mockVariantProductRepo) WithTx(_ *sql.Tx) catalog.ProductRepository { return m }

type mockVariantRepo struct {
	findByIDFn      func(ctx context.Context, id string) (*catalog.Variant, error)
	listByProductFn func(ctx context.Context, productID string, offset, limit int) ([]catalog.Variant, error)
	createFn        func(ctx context.Context, v *catalog.Variant) error
	updateFn        func(ctx context.Context, v *catalog.Variant) error
}

func (m *mockVariantRepo) FindByID(ctx context.Context, id string) (*catalog.Variant, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}
func (m *mockVariantRepo) FindBySKU(ctx context.Context, sku string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) ListByProductID(ctx context.Context, productID string, offset, limit int) ([]catalog.Variant, error) {
	if m.listByProductFn != nil {
		return m.listByProductFn(ctx, productID, offset, limit)
	}
	return nil, nil
}
func (m *mockVariantRepo) Create(ctx context.Context, v *catalog.Variant) error {
	if m.createFn != nil {
		return m.createFn(ctx, v)
	}
	return nil
}
func (m *mockVariantRepo) Update(ctx context.Context, v *catalog.Variant) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, v)
	}
	return nil
}
func (m *mockVariantRepo) WithTx(_ *sql.Tx) catalog.VariantRepository { return m }

// --- helpers ---

func existingProduct() *catalog.Product {
	return &catalog.Product{
		ID:   "prod-1",
		Name: "Widget",
		Slug: "widget",
	}
}

func productFinder() func(ctx context.Context, id string) (*catalog.Product, error) {
	return func(_ context.Context, id string) (*catalog.Product, error) {
		if id == "prod-1" {
			return existingProduct(), nil
		}
		return nil, nil
	}
}

func newVariantRouter(h *shophttp.VariantHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/products/{id}/variants", h.List())
	mux.HandleFunc("GET /api/v1/products/{id}/variants/{variantId}", h.Get())
	mux.HandleFunc("POST /api/v1/admin/products/{id}/variants", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/products/{id}/variants/{variantId}", h.Update())
	return mux
}

func variantBody(t *testing.T, v interface{}) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewReader(b)
}

// --- List tests ---

func TestVariantHandler_List_OK(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		listByProductFn: func(_ context.Context, pid string, offset, limit int) ([]catalog.Variant, error) {
			return []catalog.Variant{
				{ID: "v1", ProductID: pid, SKU: "SKU-1"},
				{ID: "v2", ProductID: pid, SKU: "SKU-2"},
			}, nil
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/prod-1/variants", nil)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Data struct {
			Variants []catalog.Variant `json:"variants"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.Variants) != 2 {
		t.Errorf("len(variants) = %d, want 2", len(resp.Data.Variants))
	}
}

func TestVariantHandler_List_ProductNotFound(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/missing/variants", nil)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestVariantHandler_List_RepoError(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		listByProductFn: func(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
			return nil, apperror.Internal("db error")
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/prod-1/variants", nil)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// --- Get tests ---

func TestVariantHandler_Get_OK(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "SKU-1", Name: "Size M"}, nil
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/prod-1/variants/v1", nil)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Data struct {
			Variant catalog.Variant `json:"variant"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Variant.SKU != "SKU-1" {
		t.Errorf("sku = %q, want SKU-1", resp.Data.Variant.SKU)
	}
}

func TestVariantHandler_Get_NotFound(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/prod-1/variants/missing", nil)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestVariantHandler_Get_WrongProduct(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{ID: id, ProductID: "other-product", SKU: "SKU-X"}, nil
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/prod-1/variants/v1", nil)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestVariantHandler_Get_ProductNotFound(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/products/missing/variants/v1", nil)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// --- Create tests ---

func TestVariantHandler_Create_OK(t *testing.T) {
	var created *catalog.Variant
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		createFn: func(_ context.Context, v *catalog.Variant) error {
			created = v
			return nil
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	body := variantBody(t, map[string]interface{}{
		"sku":  "SKU-NEW",
		"name": "Size L",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products/prod-1/variants", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if created == nil {
		t.Fatal("variant was not created")
	}
	if created.SKU != "SKU-NEW" {
		t.Errorf("sku = %q, want SKU-NEW", created.SKU)
	}
	if created.Name != "Size L" {
		t.Errorf("name = %q, want 'Size L'", created.Name)
	}
	if created.ProductID != "prod-1" {
		t.Errorf("productID = %q, want prod-1", created.ProductID)
	}
	if created.ID == "" {
		t.Error("variant ID should be generated")
	}
}

func TestVariantHandler_Create_WithAttributes(t *testing.T) {
	var created *catalog.Variant
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		createFn: func(_ context.Context, v *catalog.Variant) error {
			created = v
			return nil
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	body := variantBody(t, map[string]interface{}{
		"sku":        "SKU-ATT",
		"attributes": map[string]interface{}{"size": "XL"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products/prod-1/variants", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if created.Attributes["size"] != "XL" {
		t.Errorf("attributes[size] = %v, want XL", created.Attributes["size"])
	}
}

func TestVariantHandler_Create_MissingSKU(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	body := variantBody(t, map[string]interface{}{"name": "Size M"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products/prod-1/variants", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestVariantHandler_Create_InvalidBody(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products/prod-1/variants", bytes.NewReader([]byte("bad")))
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestVariantHandler_Create_DuplicateSKU(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		createFn: func(_ context.Context, v *catalog.Variant) error {
			return apperror.Conflict("variant with this sku already exists")
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	body := variantBody(t, map[string]interface{}{"sku": "SKU-DUP"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products/prod-1/variants", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestVariantHandler_Create_ProductNotFound(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	body := variantBody(t, map[string]interface{}{"sku": "SKU-1"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products/missing/variants", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// --- Update tests ---

func TestVariantHandler_Update_OK(t *testing.T) {
	var updated *catalog.Variant
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{
				ID: id, ProductID: "prod-1", SKU: "SKU-OLD", Name: "old",
				Attributes: map[string]interface{}{},
			}, nil
		},
		updateFn: func(_ context.Context, v *catalog.Variant) error {
			updated = v
			return nil
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	body := variantBody(t, map[string]interface{}{
		"sku":  "SKU-UPDATED",
		"name": "new name",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/prod-1/variants/v1", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if updated == nil {
		t.Fatal("variant was not updated")
	}
	if updated.SKU != "SKU-UPDATED" {
		t.Errorf("sku = %q, want SKU-UPDATED", updated.SKU)
	}
	if updated.Name != "new name" {
		t.Errorf("name = %q, want 'new name'", updated.Name)
	}
}

func TestVariantHandler_Update_NotFound(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	body := variantBody(t, map[string]interface{}{"sku": "X"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/prod-1/variants/missing", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestVariantHandler_Update_WrongProduct(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{ID: id, ProductID: "other-product", SKU: "SKU-X"}, nil
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	body := variantBody(t, map[string]interface{}{"sku": "X"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/prod-1/variants/v1", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestVariantHandler_Update_EmptySKU(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "SKU-1"}, nil
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	sku := ""
	body := variantBody(t, map[string]interface{}{"sku": sku})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/prod-1/variants/v1", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestVariantHandler_Update_InvalidBody(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "SKU-1"}, nil
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/prod-1/variants/v1", bytes.NewReader([]byte("bad")))
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestVariantHandler_Update_PartialUpdate(t *testing.T) {
	var updated *catalog.Variant
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{
				ID: id, ProductID: "prod-1", SKU: "SKU-ORIG", Name: "orig",
				Attributes: map[string]interface{}{"color": "red"},
			}, nil
		},
		updateFn: func(_ context.Context, v *catalog.Variant) error {
			updated = v
			return nil
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	// Only update name — sku and attributes stay the same.
	body := variantBody(t, map[string]interface{}{"name": "new name"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/prod-1/variants/v1", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if updated.SKU != "SKU-ORIG" {
		t.Errorf("sku = %q, want SKU-ORIG (unchanged)", updated.SKU)
	}
	if updated.Name != "new name" {
		t.Errorf("name = %q, want 'new name'", updated.Name)
	}
	if updated.Attributes["color"] != "red" {
		t.Errorf("attributes[color] = %v, want red (unchanged)", updated.Attributes["color"])
	}
}

func TestVariantHandler_Update_RepoError(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "SKU-1"}, nil
		},
		updateFn: func(_ context.Context, v *catalog.Variant) error {
			return apperror.Internal("db down")
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	body := variantBody(t, map[string]interface{}{"name": "X"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/prod-1/variants/v1", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestVariantHandler_Update_DuplicateSKU(t *testing.T) {
	prodRepo := &mockVariantProductRepo{findByIDFn: productFinder()}
	varRepo := &mockVariantRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "SKU-1"}, nil
		},
		updateFn: func(_ context.Context, v *catalog.Variant) error {
			return apperror.Conflict("variant with this sku already exists")
		},
	}
	h := shophttp.NewVariantHandler(prodRepo, varRepo)

	body := variantBody(t, map[string]interface{}{"sku": "SKU-DUP"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/prod-1/variants/v1", body)
	newVariantRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}
