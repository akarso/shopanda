package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- mock for admin tests ---

type mockAdminProductRepo struct {
	findByIDFn func(ctx context.Context, id string) (*catalog.Product, error)
	createFn   func(ctx context.Context, p *catalog.Product) error
	updateFn   func(ctx context.Context, p *catalog.Product) error
}

func (m *mockAdminProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockAdminProductRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	return nil, nil
}

func (m *mockAdminProductRepo) List(ctx context.Context, offset, limit int) ([]catalog.Product, error) {
	return nil, nil
}

func (m *mockAdminProductRepo) Create(ctx context.Context, p *catalog.Product) error {
	if m.createFn != nil {
		return m.createFn(ctx, p)
	}
	return nil
}

func (m *mockAdminProductRepo) Update(ctx context.Context, p *catalog.Product) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, p)
	}
	return nil
}
func (m *mockAdminProductRepo) WithTx(_ catalog.Tx) catalog.ProductRepository { return m }

// --- helpers ---

func newAdminRouter(h *shophttp.ProductAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/admin/products", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/products/{id}", h.Update())
	return mux
}

func jsonBody(t *testing.T, v interface{}) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewReader(b)
}

// --- Create tests ---

func TestProductAdminHandler_Create_OK(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	body := jsonBody(t, map[string]interface{}{
		"name":        "Widget",
		"slug":        "widget",
		"description": "A fine widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if created == nil {
		t.Fatal("product was not created")
	}
	if created.Name != "Widget" {
		t.Errorf("name = %q, want Widget", created.Name)
	}
	if created.Slug != "widget" {
		t.Errorf("slug = %q, want widget", created.Slug)
	}
	if created.Description != "A fine widget" {
		t.Errorf("description = %q, want 'A fine widget'", created.Description)
	}
	if created.Status != catalog.StatusDraft {
		t.Errorf("status = %q, want draft", created.Status)
	}
	if created.ID == "" {
		t.Error("product ID should be generated")
	}
}

func TestProductAdminHandler_Create_WithAttributes(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	body := jsonBody(t, map[string]interface{}{
		"name":       "Widget",
		"slug":       "widget",
		"attributes": map[string]interface{}{"color": "blue"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if created.Attributes["color"] != "blue" {
		t.Errorf("attributes[color] = %v, want blue", created.Attributes["color"])
	}
}

func TestProductAdminHandler_Create_MissingName(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := shophttp.NewProductAdminHandler(repo)

	body := jsonBody(t, map[string]interface{}{
		"slug": "widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Create_MissingSlug(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := shophttp.NewProductAdminHandler(repo)

	body := jsonBody(t, map[string]interface{}{
		"name": "Widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Create_InvalidBody(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := shophttp.NewProductAdminHandler(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", bytes.NewReader([]byte("not json")))
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Create_DuplicateSlug(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			return apperror.Conflict("product with this slug already exists")
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	body := jsonBody(t, map[string]interface{}{
		"name": "Widget",
		"slug": "widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

// --- Update tests ---

func TestProductAdminHandler_Update_OK(t *testing.T) {
	existing := &catalog.Product{
		ID:          "p1",
		Name:        "Widget",
		Slug:        "widget",
		Description: "old",
		Status:      catalog.StatusDraft,
		Attributes:  map[string]interface{}{},
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	body := jsonBody(t, map[string]interface{}{
		"name":        "Updated Widget",
		"description": "new desc",
		"status":      "active",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if updated == nil {
		t.Fatal("product was not updated")
	}
	if updated.Name != "Updated Widget" {
		t.Errorf("name = %q, want 'Updated Widget'", updated.Name)
	}
	if updated.Description != "new desc" {
		t.Errorf("description = %q, want 'new desc'", updated.Description)
	}
	if updated.Status != catalog.StatusActive {
		t.Errorf("status = %q, want active", updated.Status)
	}
}

func TestProductAdminHandler_Update_NotFound(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return nil, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	body := jsonBody(t, map[string]interface{}{"name": "X"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/missing", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestProductAdminHandler_Update_InvalidStatus(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w"}, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	body := jsonBody(t, map[string]interface{}{"status": "bogus"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Update_EmptyName(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w"}, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	name := ""
	body := jsonBody(t, map[string]interface{}{"name": name})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Update_EmptySlug(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w"}, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	slug := ""
	body := jsonBody(t, map[string]interface{}{"slug": slug})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Update_InvalidBody(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w"}, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", bytes.NewReader([]byte("bad")))
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Update_PartialUpdate(t *testing.T) {
	existing := &catalog.Product{
		ID:          "p1",
		Name:        "Widget",
		Slug:        "widget",
		Description: "desc",
		Status:      catalog.StatusDraft,
		Attributes:  map[string]interface{}{},
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	// Only update description — name, slug, status stay the same.
	body := jsonBody(t, map[string]interface{}{"description": "new desc"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if updated.Name != "Widget" {
		t.Errorf("name = %q, want Widget (unchanged)", updated.Name)
	}
	if updated.Slug != "widget" {
		t.Errorf("slug = %q, want widget (unchanged)", updated.Slug)
	}
	if updated.Description != "new desc" {
		t.Errorf("description = %q, want 'new desc'", updated.Description)
	}
	if updated.Status != catalog.StatusDraft {
		t.Errorf("status = %q, want draft (unchanged)", updated.Status)
	}
}

func TestProductAdminHandler_Update_RepoError(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w"}, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			return apperror.Internal("db down")
		},
	}
	h := shophttp.NewProductAdminHandler(repo)

	body := jsonBody(t, map[string]interface{}{"name": "X"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
