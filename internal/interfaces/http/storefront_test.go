package http_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- mock repo for storefront tests ---

type mockStorefrontRepo struct {
	findBySlugFn func(ctx context.Context, slug string) (*catalog.Product, error)
}

func (m *mockStorefrontRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	if m.findBySlugFn != nil {
		return m.findBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockStorefrontRepo) FindByID(_ context.Context, _ string) (*catalog.Product, error) {
	return nil, nil
}
func (m *mockStorefrontRepo) List(_ context.Context, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockStorefrontRepo) Create(_ context.Context, _ *catalog.Product) error { return nil }
func (m *mockStorefrontRepo) Update(_ context.Context, _ *catalog.Product) error { return nil }
func (m *mockStorefrontRepo) FindByCategoryID(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockStorefrontRepo) WithTx(_ *sql.Tx) catalog.ProductRepository { return m }

// --- test theme helpers ---

func createTestTheme(t *testing.T) *theme.Engine {
	t.Helper()
	dir := t.TempDir()

	// theme.yaml
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: test\nversion: \"0.1.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// templates directory
	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatal(err)
	}

	layout := `<!DOCTYPE html><html><head><title>{{ template "title" . }}</title></head><body>{{ template "content" . }}</body></html>`
	if err := os.WriteFile(filepath.Join(tplDir, "layout.html"), []byte(layout), 0644); err != nil {
		t.Fatal(err)
	}

	home := `{{ define "title" }}{{ .Layout.SiteName }}{{ end }}{{ define "content" }}<h1>Welcome to {{ .Layout.SiteName }}</h1>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "home.html"), []byte(home), 0644); err != nil {
		t.Fatal(err)
	}

	product := `{{ define "title" }}{{ .Product.Name }}{{ end }}{{ define "content" }}<h1>{{ .Product.Name }}</h1><p>{{ .Product.Description }}</p>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "product.html"), []byte(product), 0644); err != nil {
		t.Fatal(err)
	}

	engine, err := theme.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func newStorefrontRouter(h *shophttp.StorefrontHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.Home())
	mux.HandleFunc("GET /products/{slug}", h.Product())
	return mux
}

// --- tests ---

func TestStorefrontHandler_Product_OK(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{
				ID:          "p1",
				Name:        "Widget",
				Slug:        slug,
				Description: "A fine widget",
			}, nil
		},
	}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/widget", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/html; charset=utf-8")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "<h1>Widget</h1>") {
		t.Errorf("body missing product name heading; got: %s", body)
	}
	if !strings.Contains(body, "A fine widget") {
		t.Errorf("body missing description; got: %s", body)
	}
}

func TestStorefrontHandler_Home_OK(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Welcome to test") {
		t.Fatalf("body missing home welcome text: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_Product_NotFound(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return nil, nil
		},
	}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/nonexistent", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStorefrontHandler_Product_NotFoundError(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return nil, apperror.NotFound("product not found")
		},
	}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/gone", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStorefrontHandler_Product_RepoError(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return nil, apperror.Internal("db down")
		},
	}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/widget", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestStorefrontHandler_Product_WithPipeline(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{ID: "p1", Name: "Widget", Slug: slug}, nil
		},
	}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	pdp.AddStep(addBlockStep{name: "hero", typ: "hero_banner"})
	h := shophttp.NewStorefrontHandler(engine, repo, pdp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/widget", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	// Pipeline ran but template still renders product name.
	if !strings.Contains(rec.Body.String(), "Widget") {
		t.Errorf("body missing product name")
	}
}
