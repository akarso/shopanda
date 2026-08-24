package storefront_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/domain/translation"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

var fixedTime = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// --- mock page repository ---

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

// --- helpers ---

func newPageRouter(pub *storefront.PageHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/pages/{slug}", pub.Get())
	return mux
}

// --- mock content translation repository ---

type mockContentTranslationRepo struct {
	findByEntityAndLanguageFn func(ctx context.Context, entityID, language string) ([]translation.ContentTranslation, error)
}

func (m *mockContentTranslationRepo) FindByEntityAndLanguage(ctx context.Context, entityID, language string) ([]translation.ContentTranslation, error) {
	if m.findByEntityAndLanguageFn != nil {
		return m.findByEntityAndLanguageFn(ctx, entityID, language)
	}
	return []translation.ContentTranslation{}, nil
}

func (m *mockContentTranslationRepo) FindFieldValue(context.Context, string, string, string) (*translation.ContentTranslation, error) {
	return nil, nil
}

func (m *mockContentTranslationRepo) Upsert(context.Context, *translation.ContentTranslation) error {
	return nil
}

func (m *mockContentTranslationRepo) DeleteByEntity(context.Context, string) error {
	return nil
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
		fixedTime, fixedTime)
}

// --- public handler: Get ---

func TestPageHandler_Get_OK(t *testing.T) {
	repo := &mockPageRepo{
		findActiveBySlugFn: func(_ context.Context, slug string) (*cms.Page, error) {
			if slug != "about" {
				t.Errorf("slug = %q, want %q", slug, "about")
			}
			return testPage(), nil
		},
	}
	h := storefront.NewPageHandler(repo, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/pages/about", nil)
	newPageRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := parsePageBody(t, rec)
	data := body["data"].(map[string]interface{})
	page := data["page"].(map[string]interface{})
	if page["slug"] != "about" {
		t.Errorf("slug = %v, want about", page["slug"])
	}
	if page["title"] != "About Us" {
		t.Errorf("title = %v, want About Us", page["title"])
	}
}

func TestPageHandler_Get_NotFound(t *testing.T) {
	repo := &mockPageRepo{
		findActiveBySlugFn: func(_ context.Context, _ string) (*cms.Page, error) {
			return nil, nil
		},
	}
	h := storefront.NewPageHandler(repo, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/pages/missing", nil)
	newPageRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPageHandler_Get_RepoError(t *testing.T) {
	repo := &mockPageRepo{
		findActiveBySlugFn: func(_ context.Context, _ string) (*cms.Page, error) {
			return nil, apperror.Internal("db error")
		},
	}
	h := storefront.NewPageHandler(repo, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/pages/about", nil)
	newPageRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestPageHandler_Get_WithContentTranslation(t *testing.T) {
	repo := &mockPageRepo{
		findActiveBySlugFn: func(_ context.Context, _ string) (*cms.Page, error) {
			return testPage(), nil
		},
	}
	ct := translation.NewContentTranslator(&mockContentTranslationRepo{
		findByEntityAndLanguageFn: func(_ context.Context, entityID, lang string) ([]translation.ContentTranslation, error) {
			if entityID == "page-1" && lang == "de" {
				return []translation.ContentTranslation{
					{EntityID: entityID, Language: lang, Field: "title", Value: "Über uns"},
					{EntityID: entityID, Language: lang, Field: "content", Value: "<p>Hallo</p>"},
				}, nil
			}
			return []translation.ContentTranslation{}, nil
		},
	}, nil)
	h := storefront.NewPageHandler(repo, ct)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/pages/about", nil)
	req = req.WithContext(translation.WithLanguage(req.Context(), "de"))
	newPageRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := parsePageBody(t, rec)
	data := body["data"].(map[string]interface{})
	page := data["page"].(map[string]interface{})
	if page["title"] != "Über uns" {
		t.Errorf("title = %v, want Über uns", page["title"])
	}
	if page["content"] != "<p>Hallo</p>" {
		t.Errorf("content = %v, want <p>Hallo</p>", page["content"])
	}
}
