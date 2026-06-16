package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/store"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- category mutation audit ---

func newCategoryAuditRouter(h *shophttp.CategoryAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/admin/categories", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/categories/{id}", h.Update())
	mux.HandleFunc("DELETE /api/v1/admin/categories/{id}", h.Delete())
	return mux
}

func TestCategoryAdmin_Create_AuditsScope(t *testing.T) {
	repo := &mockCategoryRepo{}
	sink := &auditSink{}
	h := shophttp.NewCategoryAdminHandlerWithAuditor(repo, categoryBus(), adminapp.NewAuditor(sink))

	req := httptest.NewRequest("POST", "/api/v1/admin/categories", strings.NewReader(`{"name":"Electronics","slug":"electronics"}`))
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	rec := httptest.NewRecorder()
	newCategoryAuditRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditCategoryCreate {
		t.Errorf("action = %v, want %v", last.context["action"], adminapp.AuditCategoryCreate)
	}
	assertScopeTriad(t, last.context, "store-eu", "en", "USD")
}

func TestCategoryAdmin_Update_AuditsScope(t *testing.T) {
	repo := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			if id != "cat-1" {
				return nil, nil
			}
			return &catalog.Category{ID: "cat-1", Name: "Old", Slug: "old"}, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCategoryAdminHandlerWithAuditor(repo, categoryBus(), adminapp.NewAuditor(sink))

	req := httptest.NewRequest("PUT", "/api/v1/admin/categories/cat-1", strings.NewReader(`{"name":"New"}`))
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	rec := httptest.NewRecorder()
	newCategoryAuditRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditCategoryUpdate {
		t.Errorf("action = %v, want %v", last.context["action"], adminapp.AuditCategoryUpdate)
	}
	assertScopeTriad(t, last.context, "store-eu", "en", "USD")
}

func TestCategoryAdmin_Delete_AuditsScope(t *testing.T) {
	repo := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			if id != "cat-1" {
				return nil, nil
			}
			return &catalog.Category{ID: "cat-1", Name: "Old", Slug: "old"}, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCategoryAdminHandlerWithAuditor(repo, categoryBus(), adminapp.NewAuditor(sink))

	req := httptest.NewRequest("DELETE", "/api/v1/admin/categories/cat-1", nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	rec := httptest.NewRecorder()
	newCategoryAuditRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditCategoryDelete {
		t.Errorf("action = %v, want %v", last.context["action"], adminapp.AuditCategoryDelete)
	}
	assertScopeTriad(t, last.context, "store-eu", "en", "USD")
}

// --- category-product assignment audit ---

func TestCategoryAssignment_Assign_AuditsScope(t *testing.T) {
	categories := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			return &catalog.Category{ID: id, Name: "C", Slug: "c"}, nil
		},
	}
	products := &mockCatProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "P", Slug: "p"}, nil
		},
	}
	assignments := &mockProductCategoryAssignmentRepo{}
	sink := &auditSink{}
	h := shophttp.NewCategoryProductAssignmentAdminHandlerWithAuditor(categories, products, assignments, adminapp.NewAuditor(sink))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/admin/categories/{id}/products/{productId}", h.Assign())

	req := httptest.NewRequest("POST", "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditCategoryProductAssign {
		t.Errorf("action = %v, want %v", last.context["action"], adminapp.AuditCategoryProductAssign)
	}
	if last.context["detail_product_id"] != "prod-1" {
		t.Errorf("detail_product_id = %v, want prod-1", last.context["detail_product_id"])
	}
	assertScopeTriad(t, last.context, "store-eu", "en", "USD")
}

func TestCategoryAssignment_Unassign_AuditsScope(t *testing.T) {
	categories := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			return &catalog.Category{ID: id, Name: "C", Slug: "c"}, nil
		},
	}
	products := &mockCatProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "P", Slug: "p"}, nil
		},
	}
	assignments := &mockProductCategoryAssignmentRepo{}
	sink := &auditSink{}
	h := shophttp.NewCategoryProductAssignmentAdminHandlerWithAuditor(categories, products, assignments, adminapp.NewAuditor(sink))

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/admin/categories/{id}/products/{productId}", h.Unassign())

	req := httptest.NewRequest("DELETE", "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditCategoryProductUnassign {
		t.Errorf("action = %v, want %v", last.context["action"], adminapp.AuditCategoryProductUnassign)
	}
	assertScopeTriad(t, last.context, "store-eu", "en", "USD")
}

// --- store mutation audit ---

func TestStoreAdmin_Create_AuditsScope(t *testing.T) {
	repo := &mockStoreAdminRepo{}
	sink := &auditSink{}
	h := shophttp.NewStoreAdminHandlerWithAuditor(repo, storeAdminBus(), adminapp.NewAuditor(sink))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/admin/stores", h.Create())

	body := `{"code":"de","name":"Germany","currency":"EUR","country":"DE","language":"de","domain":"shop.de"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/stores", strings.NewReader(body))
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditStoreCreate {
		t.Errorf("action = %v, want %v", last.context["action"], adminapp.AuditStoreCreate)
	}
	assertScopeTriad(t, last.context, "store-eu", "en", "USD")
}

func TestStoreAdmin_Update_AuditsScope(t *testing.T) {
	now := time.Now()
	existing := store.NewStoreFromDB("s-1", "de", "Germany", "EUR", "DE", "de", "shop.de", false, now, now)
	repo := &mockStoreAdminRepo{
		findByIDFn: func(_ context.Context, id string) (*store.Store, error) {
			if id == "s-1" {
				return existing, nil
			}
			return nil, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewStoreAdminHandlerWithAuditor(repo, storeAdminBus(), adminapp.NewAuditor(sink))

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/admin/stores/{id}", h.Update())

	req := httptest.NewRequest("PUT", "/api/v1/admin/stores/s-1", strings.NewReader(`{"name":"Deutschland"}`))
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditStoreUpdate {
		t.Errorf("action = %v, want %v", last.context["action"], adminapp.AuditStoreUpdate)
	}
	assertScopeTriad(t, last.context, "store-eu", "en", "USD")
}
