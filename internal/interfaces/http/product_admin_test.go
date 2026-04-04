package http_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"

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
func (m *mockAdminProductRepo) WithTx(_ *sql.Tx) catalog.ProductRepository { return m }

// --- helpers ---

func newAdminRouter(h *shophttp.ProductAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/admin/products", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/products/{id}", h.Update())
	return mux
}

func testAdminBus() *event.Bus {
	return event.NewBus(logger.NewWithWriter(io.Discard, "error"))
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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

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
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{"name": "X"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// --- Event emission tests ---

func TestProductAdminHandler_Create_EmitsEvent(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, _ *catalog.Product) error { return nil },
	}
	bus := testAdminBus()

	var captured event.Event
	bus.On(catalog.EventProductCreated, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	h := shophttp.NewProductAdminHandler(repo, bus)
	body := jsonBody(t, map[string]interface{}{"name": "Widget", "slug": "widget"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if captured.Name != catalog.EventProductCreated {
		t.Fatalf("event name = %q, want %q", captured.Name, catalog.EventProductCreated)
	}
	data, ok := captured.Data.(catalog.ProductCreatedData)
	if !ok {
		t.Fatalf("event data type = %T, want ProductCreatedData", captured.Data)
	}
	if data.Name != "Widget" {
		t.Errorf("data.Name = %q, want Widget", data.Name)
	}
	if data.Slug != "widget" {
		t.Errorf("data.Slug = %q, want widget", data.Slug)
	}
}

func TestProductAdminHandler_Update_EmitsEvent(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Old", Slug: "old", Status: catalog.StatusDraft}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error { return nil },
	}
	bus := testAdminBus()

	var captured event.Event
	bus.On(catalog.EventProductUpdated, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	h := shophttp.NewProductAdminHandler(repo, bus)
	body := jsonBody(t, map[string]interface{}{"name": "New Name"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured.Name != catalog.EventProductUpdated {
		t.Fatalf("event name = %q, want %q", captured.Name, catalog.EventProductUpdated)
	}
	data, ok := captured.Data.(catalog.ProductUpdatedData)
	if !ok {
		t.Fatalf("event data type = %T, want ProductUpdatedData", captured.Data)
	}
	if data.Name != "New Name" {
		t.Errorf("data.Name = %q, want 'New Name'", data.Name)
	}
}

// ── admin route guard tests ────────────────────────────────────────────

func createProductBody(t *testing.T) *bytes.Reader {
	t.Helper()
	return jsonBody(t, map[string]interface{}{"name": "Widget", "slug": "widget"})
}

func newGuardedAdminRouter(h *shophttp.ProductAdminHandler) *http.ServeMux {
	requireAdmin := shophttp.RequireRole(identity.RoleAdmin)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/products", requireAdmin(h.Create()))
	mux.Handle("PUT /api/v1/admin/products/{id}", requireAdmin(h.Update()))
	return mux
}

func TestAdminGuard_CustomerForbidden(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	req = testhelper.CustomerRequest(req, "cust-1")
	newGuardedAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAdminGuard_GuestUnauthorized(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	newGuardedAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAdminGuard_AdminAllowed(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	req = testhelper.AdminRequest(req, "admin-1")
	newGuardedAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil {
		t.Fatal("product should have been created")
	}
}

// ── integration test: AuthMiddleware → RequireRole(admin) ──────────────

// stubAdminTokenParser parses test tokens of the form "test-token:<userID>:<role>".
type stubAdminTokenParser struct{}

func (p *stubAdminTokenParser) Parse(_ context.Context, token string) (identity.Identity, error) {
	parts := strings.SplitN(token, ":", 3)
	if len(parts) != 3 || parts[0] != "test-token" {
		return identity.Identity{}, errors.New("invalid test token")
	}
	role := identity.Role(parts[2])
	if !role.IsValid() {
		return identity.Identity{}, errors.New("invalid role: " + parts[2])
	}
	return identity.NewIdentity(parts[1], role)
}

func newIntegrationAdminRouter(h *shophttp.ProductAdminHandler) http.Handler {
	requireAdmin := shophttp.RequireRole(identity.RoleAdmin)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/products", requireAdmin(h.Create()))

	authMW := shophttp.AuthMiddleware(&stubAdminTokenParser{})
	return authMW(mux)
}

func TestAdminGuard_Integration_NoToken(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	newIntegrationAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAdminGuard_Integration_CustomerToken(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	req.Header.Set("Authorization", "Bearer test-token:cust-1:customer")
	newIntegrationAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAdminGuard_Integration_AdminToken(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	req.Header.Set("Authorization", "Bearer test-token:admin-1:admin")
	newIntegrationAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil {
		t.Fatal("product should have been created")
	}
}
