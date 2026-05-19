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

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- mock for admin tests ---

type mockAdminProductRepo struct {
	findByIDFn                 func(ctx context.Context, id string) (*catalog.Product, error)
	listFn                     func(ctx context.Context, offset, limit int) ([]catalog.Product, error)
	createFn                   func(ctx context.Context, p *catalog.Product) error
	updateFn                   func(ctx context.Context, p *catalog.Product) error
	listCategoryIDsByProductFn func(ctx context.Context, productID string) ([]string, error)
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
	if m.listFn != nil {
		return m.listFn(ctx, offset, limit)
	}
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
func (m *mockAdminProductRepo) ListCategoryIDsByProduct(ctx context.Context, productID string) ([]string, error) {
	if m.listCategoryIDsByProductFn != nil {
		return m.listCategoryIDsByProductFn(ctx, productID)
	}
	return nil, nil
}
func (m *mockAdminProductRepo) FindByCategoryID(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockAdminProductRepo) WithTx(_ *sql.Tx) catalog.ProductRepository { return m }

// --- helpers ---

func newAdminRouter(h *shophttp.ProductAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/products", h.List())
	mux.HandleFunc("GET /api/v1/admin/products/{id}", h.Get())
	mux.HandleFunc("POST /api/v1/admin/products", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/products/{id}", h.Update())
	return mux
}

func newAdminRouterWithAudit(h *shophttp.ProductAdminHandler) *http.ServeMux {
	requireProductsRead := shophttp.RequirePermission(rbac.ProductsRead)
	requireProductsWrite := shophttp.RequirePermission(rbac.ProductsWrite)
	withAdminContext := shophttp.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/products", withAdminContext(requireProductsRead(h.List())))
	mux.Handle("GET /api/v1/admin/products/{id}", withAdminContext(requireProductsRead(h.Get())))
	mux.Handle("PUT /api/v1/admin/products/{id}", withAdminContext(requireProductsWrite(h.Update())))
	return mux
}

func newAdminCreateRouterWithAudit(h *shophttp.ProductAdminHandler) *http.ServeMux {
	requireProductsWrite := shophttp.RequirePermission(rbac.ProductsWrite)
	withAdminContext := shophttp.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/products", withAdminContext(requireProductsWrite(h.Create())))
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

// --- List tests ---

func TestProductAdminHandler_Get_OK(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			if id != "p1" {
				t.Fatalf("id = %q, want %q", id, "p1")
			}
			return &catalog.Product{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive}, nil
		},
		listCategoryIDsByProductFn: func(_ context.Context, productID string) ([]string, error) {
			if productID != "p1" {
				t.Fatalf("productID = %q, want %q", productID, "p1")
			}
			return []string{"cat-1", "cat-2"}, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data is not an object: %v", body)
	}
	product, ok := data["product"].(map[string]interface{})
	if !ok {
		t.Fatalf("product is not an object: %v", data)
	}
	if product["ID"] != "p1" {
		t.Fatalf("product ID = %v, want %q", product["ID"], "p1")
	}
	categoryIDs, ok := data["category_ids"].([]interface{})
	if !ok {
		t.Fatalf("category_ids is not an array: %v", data)
	}
	if len(categoryIDs) != 2 {
		t.Fatalf("category_ids len = %d, want 2", len(categoryIDs))
	}
}

func TestProductAdminHandler_Get_NotFound(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestProductAdminHandler_List_OK(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			if offset != 0 {
				t.Errorf("offset = %d, want 0", offset)
			}
			if limit != 20 {
				t.Errorf("limit = %d, want 20", limit)
			}
			return []catalog.Product{
				{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive},
				{ID: "p2", Name: "Gadget", Slug: "gadget", Status: catalog.StatusDraft},
			}, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data is not an object: %v", body)
	}
	products, ok := data["products"].([]interface{})
	if !ok {
		t.Fatalf("products is not an array: %v", data)
	}
	if len(products) != 2 {
		t.Fatalf("products len = %d, want 2", len(products))
	}
}

func TestProductAdminHandler_List_Empty(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, _, _ int) ([]catalog.Product, error) {
			return nil, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestProductAdminHandler_List_Pagination(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			if offset != 10 {
				t.Errorf("offset = %d, want 10", offset)
			}
			if limit != 5 {
				t.Errorf("limit = %d, want 5", limit)
			}
			return nil, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?offset=10&limit=5", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestProductAdminHandler_List_AuditIncludesScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			return []catalog.Product{{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive}}, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?offset=2&limit=7", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != admin.AuditProductRead {
		t.Errorf("action = %v, want %q", got, admin.AuditProductRead)
	}
	if got := entry.context["detail_offset"]; got != 2 {
		t.Errorf("detail_offset = %v, want %d", got, 2)
	}
	if got := entry.context["detail_limit"]; got != 7 {
		t.Errorf("detail_limit = %v, want %d", got, 7)
	}
	if got := entry.context["detail_store_id"]; got != "store-eu" {
		t.Errorf("detail_store_id = %v, want %q", got, "store-eu")
	}
	if got := entry.context["detail_language"]; got != "en" {
		t.Errorf("detail_language = %v, want %q", got, "en")
	}
	if got := entry.context["detail_currency"]; got != "EUR" {
		t.Errorf("detail_currency = %v, want %q", got, "EUR")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["resource_type"]; got != "products" {
		t.Errorf("resource_type = %v, want %q", got, "products")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
}

func TestProductAdminHandler_List_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			return []catalog.Product{{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive}}, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?offset=2&limit=7", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != admin.AuditProductRead {
		t.Errorf("action = %v, want %q", got, admin.AuditProductRead)
	}
	if got := entry.context["detail_offset"]; got != 2 {
		t.Errorf("detail_offset = %v, want %d", got, 2)
	}
	if got := entry.context["detail_limit"]; got != 7 {
		t.Errorf("detail_limit = %v, want %d", got, 7)
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["resource_type"]; got != "products" {
		t.Errorf("resource_type = %v, want %q", got, "products")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
	if _, ok := entry.context["detail_store_id"]; ok {
		t.Errorf("detail_store_id present = %v, want absent", entry.context["detail_store_id"])
	}
	if _, ok := entry.context["detail_language"]; ok {
		t.Errorf("detail_language present = %v, want absent", entry.context["detail_language"])
	}
	if _, ok := entry.context["detail_currency"]; ok {
		t.Errorf("detail_currency present = %v, want absent", entry.context["detail_currency"])
	}
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

func TestProductAdminHandler_Create_AuditIncludesScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			return nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	body := jsonBody(t, map[string]interface{}{
		"name": "Widget",
		"slug": "widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newAdminCreateRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != admin.AuditProductCreate {
		t.Errorf("action = %v, want %q", got, admin.AuditProductCreate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["resource_id"]; got == nil || got == "" {
		t.Errorf("resource_id = %v, want non-empty", got)
	}
	if got := entry.context["detail_store_id"]; got != "store-eu" {
		t.Errorf("detail_store_id = %v, want %q", got, "store-eu")
	}
	if got := entry.context["detail_language"]; got != "en" {
		t.Errorf("detail_language = %v, want %q", got, "en")
	}
	if got := entry.context["detail_currency"]; got != "EUR" {
		t.Errorf("detail_currency = %v, want %q", got, "EUR")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
}

func TestProductAdminHandler_Create_AuditFailureIncludesError(t *testing.T) {
	repo := &mockAdminProductRepo{}
	sink := &auditSink{}
	h := shophttp.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", bytes.NewReader([]byte("not json")))
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newAdminCreateRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != admin.AuditProductCreate {
		t.Errorf("action = %v, want %q", got, admin.AuditProductCreate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got == nil || got == "" {
		t.Errorf("error = %v, want non-empty", got)
	}
	if got := entry.context["detail_store_id"]; got != "store-eu" {
		t.Errorf("detail_store_id = %v, want %q", got, "store-eu")
	}
	if got := entry.context["detail_language"]; got != "en" {
		t.Errorf("detail_language = %v, want %q", got, "en")
	}
	if got := entry.context["detail_currency"]; got != "EUR" {
		t.Errorf("detail_currency = %v, want %q", got, "EUR")
	}
}

func TestProductAdminHandler_Create_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			return nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", bytes.NewReader([]byte(`{"name":"Widget","slug":"widget"}`)))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newAdminCreateRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != admin.AuditProductCreate {
		t.Errorf("action = %v, want %q", got, admin.AuditProductCreate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["resource_id"]; got == nil || got == "" {
		t.Errorf("resource_id = %v, want non-empty", got)
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
	if _, ok := entry.context["detail_store_id"]; ok {
		t.Errorf("detail_store_id present = %v, want absent", entry.context["detail_store_id"])
	}
	if _, ok := entry.context["detail_language"]; ok {
		t.Errorf("detail_language present = %v, want absent", entry.context["detail_language"])
	}
	if _, ok := entry.context["detail_currency"]; ok {
		t.Errorf("detail_currency present = %v, want absent", entry.context["detail_currency"])
	}
	if _, ok := entry.context["error"]; ok {
		t.Errorf("error present = %v, want absent", entry.context["error"])
	}
}

func TestProductAdminHandler_Create_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{}
	sink := &auditSink{}
	h := shophttp.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", bytes.NewReader([]byte("not json")))
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newAdminCreateRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != admin.AuditProductCreate {
		t.Errorf("action = %v, want %q", got, admin.AuditProductCreate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["error"]; got == nil || got == "" {
		t.Errorf("error = %v, want non-empty", got)
	}
	if _, ok := entry.context["detail_store_id"]; ok {
		t.Errorf("detail_store_id present = %v, want absent", entry.context["detail_store_id"])
	}
	if _, ok := entry.context["detail_language"]; ok {
		t.Errorf("detail_language present = %v, want absent", entry.context["detail_language"])
	}
	if _, ok := entry.context["detail_currency"]; ok {
		t.Errorf("detail_currency present = %v, want absent", entry.context["detail_currency"])
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

func TestProductAdminHandler_Update_AuditIncludesScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft}, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			return nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	body := jsonBody(t, map[string]interface{}{"status": "active"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != admin.AuditProductUpdate {
		t.Errorf("action = %v, want %q", got, admin.AuditProductUpdate)
	}
	if got := entry.context["resource_id"]; got != "p1" {
		t.Errorf("resource_id = %v, want %q", got, "p1")
	}
	if got := entry.context["detail_store_id"]; got != "store-eu" {
		t.Errorf("detail_store_id = %v, want %q", got, "store-eu")
	}
	if got := entry.context["detail_language"]; got != "en" {
		t.Errorf("detail_language = %v, want %q", got, "en")
	}
	if got := entry.context["detail_currency"]; got != "EUR" {
		t.Errorf("detail_currency = %v, want %q", got, "EUR")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
}

func TestProductAdminHandler_Update_AuditFailureIncludesError(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft}, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", bytes.NewReader([]byte("bad")))
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != admin.AuditProductUpdate {
		t.Errorf("action = %v, want %q", got, admin.AuditProductUpdate)
	}
	if got := entry.context["resource_id"]; got != "p1" {
		t.Errorf("resource_id = %v, want %q", got, "p1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got == nil || got == "" {
		t.Errorf("error = %v, want non-empty", got)
	}
	if got := entry.context["detail_store_id"]; got != "store-eu" {
		t.Errorf("detail_store_id = %v, want %q", got, "store-eu")
	}
	if got := entry.context["detail_language"]; got != "en" {
		t.Errorf("detail_language = %v, want %q", got, "en")
	}
	if got := entry.context["detail_currency"]; got != "EUR" {
		t.Errorf("detail_currency = %v, want %q", got, "EUR")
	}
}

func TestProductAdminHandler_Update_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft}, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			return nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	body := jsonBody(t, map[string]interface{}{"status": "active"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != admin.AuditProductUpdate {
		t.Errorf("action = %v, want %q", got, admin.AuditProductUpdate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["resource_id"]; got != "p1" {
		t.Errorf("resource_id = %v, want %q", got, "p1")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
	if _, ok := entry.context["detail_store_id"]; ok {
		t.Errorf("detail_store_id present = %v, want absent", entry.context["detail_store_id"])
	}
	if _, ok := entry.context["detail_language"]; ok {
		t.Errorf("detail_language present = %v, want absent", entry.context["detail_language"])
	}
	if _, ok := entry.context["detail_currency"]; ok {
		t.Errorf("detail_currency present = %v, want absent", entry.context["detail_currency"])
	}
	if _, ok := entry.context["error"]; ok {
		t.Errorf("error present = %v, want absent", entry.context["error"])
	}
}

func TestProductAdminHandler_Update_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft}, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	body := jsonBody(t, map[string]interface{}{"status": "invalid"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != admin.AuditProductUpdate {
		t.Errorf("action = %v, want %q", got, admin.AuditProductUpdate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["resource_id"]; got != "p1" {
		t.Errorf("resource_id = %v, want %q", got, "p1")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got == nil || got == "" {
		t.Errorf("error = %v, want non-empty", got)
	}
	if _, ok := entry.context["detail_store_id"]; ok {
		t.Errorf("detail_store_id present = %v, want absent", entry.context["detail_store_id"])
	}
	if _, ok := entry.context["detail_language"]; ok {
		t.Errorf("detail_language present = %v, want absent", entry.context["detail_language"])
	}
	if _, ok := entry.context["detail_currency"]; ok {
		t.Errorf("detail_currency present = %v, want absent", entry.context["detail_currency"])
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

// ── product permission guard tests ─────────────────────────────────────

func createProductBody(t *testing.T) *bytes.Reader {
	t.Helper()
	return jsonBody(t, map[string]interface{}{"name": "Widget", "slug": "widget"})
}

func newGuardedAdminRouter(h *shophttp.ProductAdminHandler) *http.ServeMux {
	requireProductsRead := shophttp.RequirePermission(rbac.ProductsRead)
	requireProductsWrite := shophttp.RequirePermission(rbac.ProductsWrite)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/products", requireProductsRead(h.List()))
	mux.Handle("POST /api/v1/admin/products", requireProductsWrite(h.Create()))
	mux.Handle("PUT /api/v1/admin/products/{id}", requireProductsWrite(h.Update()))
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

func TestAdminGuard_SupportListAllowed(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			if offset != 0 {
				t.Errorf("offset = %d, want 0", offset)
			}
			if limit != 20 {
				t.Errorf("limit = %d, want 20", limit)
			}
			return []catalog.Product{{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive}}, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	newGuardedAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAdminGuard_EditorAllowed(t *testing.T) {
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
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	newGuardedAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil {
		t.Fatal("product should have been created")
	}
}

// ── integration test: AuthMiddleware → RequirePermission(...) ──────────

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
	requireProductsRead := shophttp.RequirePermission(rbac.ProductsRead)
	requireProductsWrite := shophttp.RequirePermission(rbac.ProductsWrite)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/products", requireProductsRead(h.List()))
	mux.Handle("POST /api/v1/admin/products", requireProductsWrite(h.Create()))

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

func TestAdminGuard_Integration_SupportTokenListAllowed(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			if offset != 0 {
				t.Errorf("offset = %d, want 0", offset)
			}
			if limit != 20 {
				t.Errorf("limit = %d, want 20", limit)
			}
			return []catalog.Product{{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive}}, nil
		},
	}
	h := shophttp.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	req.Header.Set("Authorization", "Bearer test-token:support-1:support")
	newIntegrationAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAdminGuard_Integration_EditorToken(t *testing.T) {
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
	req.Header.Set("Authorization", "Bearer test-token:editor-1:editor")
	newIntegrationAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil {
		t.Fatal("product should have been created")
	}
}
