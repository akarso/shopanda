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
	"github.com/akarso/shopanda/internal/domain/search"
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
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: test\nversion: \"0.1.0\"\nstorefront:\n  search_action: /catalog\n  cart_url: /basket\n  cart_label: Basket (2)\n  nav:\n    - label: Start\n      url: /\n    - label: Browse\n      url: /categories\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// templates directory
	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatal(err)
	}

	layout := `<!DOCTYPE html><html><head><title>{{ template "title" . }}</title></head><body><nav>{{ range .Layout.Nav }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}</nav><form action="{{ .Layout.SearchAction }}"></form><span>{{ .Layout.CartLabel }}</span>{{ template "content" . }}</body></html>`
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

	listing := `{{ define "title" }}{{ .Title }}{{ end }}{{ define "content" }}<h1>{{ .Title }}</h1><p>{{ .ResultSummary }}</p><div class="view-{{ .View }}">{{ range .Products }}<article><a href="/products/{{ .Slug }}">{{ .Name }}</a><span>{{ .PriceText }}</span><small>{{ .Availability }}</small></article>{{ else }}<p>{{ .EmptyMessage }}</p>{{ end }}</div><nav>{{ range .SortOptions }}{{ if .Selected }}<strong>{{ .Label }}</strong>{{ else }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}{{ end }}</nav><div>{{ range .Pagination.Links }}{{ if .Current }}<strong>{{ .Label }}</strong>{{ else }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}{{ end }}</div>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "product_list.html"), []byte(listing), 0644); err != nil {
		t.Fatal(err)
	}

	engine, err := theme.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func createTestThemeWithoutHome(t *testing.T) *theme.Engine {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: test\nversion: \"0.1.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatal(err)
	}

	layout := `<!DOCTYPE html><html><head><title>{{ template "title" . }}</title></head><body>{{ template "content" . }}</body></html>`
	if err := os.WriteFile(filepath.Join(tplDir, "layout.html"), []byte(layout), 0644); err != nil {
		t.Fatal(err)
	}

	product := `{{ define "title" }}{{ .Product.Name }}{{ end }}{{ define "content" }}<h1>{{ .Product.Name }}</h1>{{ end }}{{ template "layout.html" . }}`
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
	mux.HandleFunc("GET /{$}", h.Home())
	mux.HandleFunc("GET /products", h.Products())
	mux.HandleFunc("GET /products/{slug}", h.Product())
	mux.HandleFunc("GET /search", h.Search())
	return mux
}

func newStorefrontSearchMock() *mockSearchEngine {
	return &mockSearchEngine{searchFn: func(_ context.Context, _ search.SearchQuery) (search.SearchResult, error) {
		return search.SearchResult{Products: []search.Product{}, Facets: map[string][]search.FacetValue{}}, nil
	}}
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
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, newStorefrontSearchMock())

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
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Welcome to test") {
		t.Fatalf("body missing home welcome text: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Basket (2)") {
		t.Fatalf("body missing configured cart label: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_Home_MissingTemplate(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestThemeWithoutHome(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestStorefrontRouter_Home_DoesNotCatchUnknownPath(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/missing", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestStorefrontHandler_Products_OK(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	plp.AddStep(addListingBlockStep{name: "listing", typ: "product_grid"})
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if query.Text != "" {
			t.Fatalf("query.Text = %q, want empty", query.Text)
		}
		if query.Limit != 12 {
			t.Fatalf("query.Limit = %d, want 12", query.Limit)
		}
		if query.Offset != 0 {
			t.Fatalf("query.Offset = %d, want 0", query.Offset)
		}
		if query.Sort != "-created_at" {
			t.Fatalf("query.Sort = %q, want -created_at", query.Sort)
		}
		return search.SearchResult{
			Products: []search.Product{{Name: "Widget", Slug: "widget", Attributes: map[string]interface{}{"image_url": "/media/widget.jpg"}}},
			Total:    1,
			Facets:   map[string][]search.FacetValue{"category": []search.FacetValue{{Value: "Shoes", Count: 1}}},
		}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, searchEngine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "All Products") {
		t.Fatalf("body missing listing title: %s", body)
	}
	if !strings.Contains(body, "Widget") {
		t.Fatalf("body missing product card: %s", body)
	}
	if !strings.Contains(body, "Newest") {
		t.Fatalf("body missing sort option label: %s", body)
	}
}

func TestStorefrontHandler_Search_OK(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if query.Text != "boots" {
			t.Fatalf("query.Text = %q, want boots", query.Text)
		}
		if query.Sort != "name" {
			t.Fatalf("query.Sort = %q, want name", query.Sort)
		}
		if query.Limit != 24 {
			t.Fatalf("query.Limit = %d, want 24", query.Limit)
		}
		if query.Offset != 24 {
			t.Fatalf("query.Offset = %d, want 24", query.Offset)
		}
		return search.SearchResult{
			Products: []search.Product{{Name: "Trail Boot", Slug: "trail-boot", Price: 12999, InStock: true}},
			Total:    26,
			Facets:   map[string][]search.FacetValue{},
		}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, searchEngine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/search?q=boots&sort=name_asc&page=2&per_page=24&view=list", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Search results for &#34;boots&#34;") {
		t.Fatalf("body missing search title: %s", body)
	}
	if !strings.Contains(body, "Trail Boot") {
		t.Fatalf("body missing search product: %s", body)
	}
	if !strings.Contains(body, "EUR 129.99") {
		t.Fatalf("body missing formatted price: %s", body)
	}
	if !strings.Contains(body, "In stock") {
		t.Fatalf("body missing availability text: %s", body)
	}
}

func TestStorefrontHandler_Products_InvalidPagination(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products?page=0", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
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
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, newStorefrontSearchMock())

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
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, newStorefrontSearchMock())

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
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, newStorefrontSearchMock())

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
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, pdp, plp, newStorefrontSearchMock())

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
