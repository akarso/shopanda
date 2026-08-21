package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

var pageFixedTime = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// mockPageRepo duplicates the storefront page_test.go mock — unexported, so
// it can't be shared across the http_test/admin_test package boundary
// created by the admin package split.
type mockPageRepo struct {
	findByIDFn         func(ctx context.Context, id string) (*cms.Page, error)
	findBySlugFn       func(ctx context.Context, slug string) (*cms.Page, error)
	findActiveBySlugFn func(ctx context.Context, slug string) (*cms.Page, error)
	listFn             func(ctx context.Context, offset, limit int) ([]*cms.Page, error)
	createFn           func(ctx context.Context, p *cms.Page) error
	updateFn           func(ctx context.Context, p *cms.Page) error
	deleteFn           func(ctx context.Context, id string) error
}

func (m *mockPageRepo) FindByID(ctx context.Context, id string) (*cms.Page, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockPageRepo) FindBySlug(ctx context.Context, slug string) (*cms.Page, error) {
	if m.findBySlugFn != nil {
		return m.findBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockPageRepo) FindActiveBySlug(ctx context.Context, slug string) (*cms.Page, error) {
	if m.findActiveBySlugFn != nil {
		return m.findActiveBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockPageRepo) List(ctx context.Context, offset, limit int) ([]*cms.Page, error) {
	if m.listFn != nil {
		return m.listFn(ctx, offset, limit)
	}
	return nil, nil
}

func (m *mockPageRepo) Create(ctx context.Context, p *cms.Page) error {
	if m.createFn != nil {
		return m.createFn(ctx, p)
	}
	return nil
}

func (m *mockPageRepo) Update(ctx context.Context, p *cms.Page) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, p)
	}
	return nil
}

func (m *mockPageRepo) Delete(ctx context.Context, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func newPageAdminRouter(h *admin.PageAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/pages", h.List())
	mux.HandleFunc("POST /api/v1/admin/pages", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/pages/{id}", h.Update())
	mux.HandleFunc("DELETE /api/v1/admin/pages/{id}", h.Delete())
	return mux
}

func pageBus() *event.Bus {
	return event.NewBus(logger.NewWithWriter(io.Discard, "error"))
}

func pageBody(t *testing.T, v interface{}) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewReader(b)
}

func parsePageBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return body
}

func testPage() *cms.Page {
	return cms.NewPageFromDB("page-1", "about", "About Us", "<p>Hello</p>", "", true,
		pageFixedTime, pageFixedTime)
}

// --- admin handler: List ---

func TestPageAdminHandler_List_OK(t *testing.T) {
	repo := &mockPageRepo{
		listFn: func(_ context.Context, _, _ int) ([]*cms.Page, error) {
			return []*cms.Page{testPage()}, nil
		},
	}
	h := admin.NewPageAdminHandler(repo, pageBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/pages", nil)
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := parsePageBody(t, rec)
	data := body["data"].(map[string]interface{})
	pages := data["pages"].([]interface{})
	if len(pages) != 1 {
		t.Fatalf("pages len = %d, want 1", len(pages))
	}
}

// --- admin handler: Create ---

func TestPageAdminHandler_Create_OK(t *testing.T) {
	var created *cms.Page
	repo := &mockPageRepo{
		createFn: func(_ context.Context, p *cms.Page) error {
			created = p
			return nil
		},
	}
	h := admin.NewPageAdminHandler(repo, pageBus())

	body := pageBody(t, map[string]interface{}{
		"slug":    "about",
		"title":   "About Us",
		"content": "<p>Hello</p>",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/pages", body)
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil {
		t.Fatal("page was not created")
	}
	if created.Slug() != "about" {
		t.Errorf("slug = %q, want about", created.Slug())
	}
	if created.Title() != "About Us" {
		t.Errorf("title = %q, want About Us", created.Title())
	}
	if created.Content() != "<p>Hello</p>" {
		t.Errorf("content = %q, want <p>Hello</p>", created.Content())
	}
	if created.ID() == "" {
		t.Error("page ID should be generated")
	}
}

func TestPageAdminHandler_Create_MissingTitle(t *testing.T) {
	repo := &mockPageRepo{}
	h := admin.NewPageAdminHandler(repo, pageBus())

	body := pageBody(t, map[string]interface{}{
		"slug": "about",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/pages", body)
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestPageAdminHandler_Create_MissingSlug(t *testing.T) {
	repo := &mockPageRepo{}
	h := admin.NewPageAdminHandler(repo, pageBus())

	body := pageBody(t, map[string]interface{}{
		"title": "About",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/pages", body)
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// --- admin handler: Update ---

func TestPageAdminHandler_Update_OK(t *testing.T) {
	existing := testPage()
	var updated *cms.Page
	repo := &mockPageRepo{
		findByIDFn: func(_ context.Context, id string) (*cms.Page, error) {
			if id != "page-1" {
				return nil, nil
			}
			return existing, nil
		},
		updateFn: func(_ context.Context, p *cms.Page) error {
			updated = p
			return nil
		},
	}
	h := admin.NewPageAdminHandler(repo, pageBus())

	newTitle := "Updated Title"
	body := pageBody(t, map[string]interface{}{
		"title": newTitle,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/pages/page-1", body)
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if updated == nil {
		t.Fatal("page was not updated")
	}
	if updated.Title() != newTitle {
		t.Errorf("title = %q, want %q", updated.Title(), newTitle)
	}
}

func TestPageAdminHandler_Update_NotFound(t *testing.T) {
	repo := &mockPageRepo{
		findByIDFn: func(_ context.Context, _ string) (*cms.Page, error) {
			return nil, nil
		},
	}
	h := admin.NewPageAdminHandler(repo, pageBus())

	body := pageBody(t, map[string]interface{}{"title": "New"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/pages/nope", body)
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// --- admin handler: Delete ---

func TestPageAdminHandler_Delete_OK(t *testing.T) {
	var deletedID string
	repo := &mockPageRepo{
		findByIDFn: func(_ context.Context, id string) (*cms.Page, error) {
			if id == "page-1" {
				return testPage(), nil
			}
			return nil, nil
		},
		deleteFn: func(_ context.Context, id string) error {
			deletedID = id
			return nil
		},
	}
	h := admin.NewPageAdminHandler(repo, pageBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/pages/page-1", nil)
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if deletedID != "page-1" {
		t.Errorf("deleted id = %q, want page-1", deletedID)
	}
}

func TestPageAdminHandler_Delete_NotFound(t *testing.T) {
	repo := &mockPageRepo{
		findByIDFn: func(_ context.Context, _ string) (*cms.Page, error) {
			return nil, nil
		},
	}
	h := admin.NewPageAdminHandler(repo, pageBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/pages/page-1", nil)
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// --- admin handler: language scope + scoped audit ---

func pageAdminWithSink(repo cms.PageRepository, sink logger.Logger) *admin.PageAdminHandler {
	return admin.NewPageAdminHandlerWithAuditor(repo, pageBus(), adminapp.NewAuditor(sink))
}

func TestPageAdminHandler_Create_PersistsLanguageWithScopedAudit(t *testing.T) {
	var created *cms.Page
	repo := &mockPageRepo{
		createFn: func(_ context.Context, p *cms.Page) error {
			created = p
			return nil
		},
	}
	sink := &auditSink{}
	h := pageAdminWithSink(repo, sink)

	body := pageBody(t, map[string]interface{}{
		"slug":     "a-propos",
		"title":    "À propos",
		"content":  "<p>Bonjour</p>",
		"language": "fr",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/pages", body)
	req = withAdminFullScope(req, "admin-1", "store-eu", "fr", "EUR")
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil {
		t.Fatal("page was not created")
	}
	if created.Language() != "fr" {
		t.Fatalf("created language = %q, want fr", created.Language())
	}
	data := parsePageBody(t, rec)["data"].(map[string]interface{})
	page := data["page"].(map[string]interface{})
	if page["language"] != "fr" {
		t.Errorf("response language = %v, want fr", page["language"])
	}

	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditPageCreate {
		t.Errorf("audit action = %v, want %v", last.context["action"], adminapp.AuditPageCreate)
	}
	assertScopeTriad(t, last.context, "store-eu", "fr", "EUR")
}

func TestPageAdminHandler_Update_PersistsLanguageWithScopedAudit(t *testing.T) {
	existing := testPage()
	var updated *cms.Page
	repo := &mockPageRepo{
		findByIDFn: func(_ context.Context, id string) (*cms.Page, error) {
			if id != "page-1" {
				return nil, nil
			}
			return existing, nil
		},
		updateFn: func(_ context.Context, p *cms.Page) error {
			updated = p
			return nil
		},
	}
	sink := &auditSink{}
	h := pageAdminWithSink(repo, sink)

	body := pageBody(t, map[string]interface{}{"language": "de"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/pages/page-1", body)
	req = withAdminFullScope(req, "admin-1", "store-de", "de", "EUR")
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if updated == nil {
		t.Fatal("page was not updated")
	}
	if updated.Language() != "de" {
		t.Fatalf("updated language = %q, want de", updated.Language())
	}

	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditPageUpdate {
		t.Errorf("audit action = %v, want %v", last.context["action"], adminapp.AuditPageUpdate)
	}
	assertScopeTriad(t, last.context, "store-de", "de", "EUR")
}

func TestPageAdminHandler_Delete_ScopedAudit(t *testing.T) {
	repo := &mockPageRepo{
		findByIDFn: func(_ context.Context, id string) (*cms.Page, error) {
			if id == "page-1" {
				return testPage(), nil
			}
			return nil, nil
		},
		deleteFn: func(_ context.Context, _ string) error { return nil },
	}
	sink := &auditSink{}
	h := pageAdminWithSink(repo, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/pages/page-1", nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	newPageAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditPageDelete {
		t.Errorf("audit action = %v, want %v", last.context["action"], adminapp.AuditPageDelete)
	}
	assertScopeTriad(t, last.context, "store-eu", "en", "USD")
}
