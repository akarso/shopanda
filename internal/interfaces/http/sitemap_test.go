package http_test

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/cms"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- mocks ---

type stubSitemapProductRepo struct {
	products []catalog.Product
	err      error
}

func (m *stubSitemapProductRepo) FindByID(_ context.Context, _ string) (*catalog.Product, error) {
	return nil, nil
}
func (m *stubSitemapProductRepo) FindBySlug(_ context.Context, _ string) (*catalog.Product, error) {
	return nil, nil
}
func (m *stubSitemapProductRepo) List(_ context.Context, _, _ int) ([]catalog.Product, error) {
	return m.products, m.err
}
func (m *stubSitemapProductRepo) FindByCategoryID(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *stubSitemapProductRepo) Create(_ context.Context, _ *catalog.Product) error { return nil }
func (m *stubSitemapProductRepo) Update(_ context.Context, _ *catalog.Product) error { return nil }

type stubSitemapCategoryRepo struct {
	categories []catalog.Category
	err        error
}

func (m *stubSitemapCategoryRepo) FindByID(_ context.Context, _ string) (*catalog.Category, error) {
	return nil, nil
}
func (m *stubSitemapCategoryRepo) FindBySlug(_ context.Context, _ string) (*catalog.Category, error) {
	return nil, nil
}
func (m *stubSitemapCategoryRepo) FindByParentID(_ context.Context, _ *string) ([]catalog.Category, error) {
	return nil, nil
}
func (m *stubSitemapCategoryRepo) FindAll(_ context.Context) ([]catalog.Category, error) {
	return m.categories, m.err
}
func (m *stubSitemapCategoryRepo) Create(_ context.Context, _ *catalog.Category) error { return nil }
func (m *stubSitemapCategoryRepo) Update(_ context.Context, _ *catalog.Category) error { return nil }

type stubSitemapPageRepo struct {
	pages []*cms.Page
	err   error
}

func (m *stubSitemapPageRepo) FindByID(_ context.Context, _ string) (*cms.Page, error) {
	return nil, nil
}
func (m *stubSitemapPageRepo) FindBySlug(_ context.Context, _ string) (*cms.Page, error) {
	return nil, nil
}
func (m *stubSitemapPageRepo) FindActiveBySlug(_ context.Context, _ string) (*cms.Page, error) {
	return nil, nil
}
func (m *stubSitemapPageRepo) List(_ context.Context, _, _ int) ([]*cms.Page, error) {
	return m.pages, m.err
}
func (m *stubSitemapPageRepo) Create(_ context.Context, _ *cms.Page) error { return nil }
func (m *stubSitemapPageRepo) Update(_ context.Context, _ *cms.Page) error { return nil }
func (m *stubSitemapPageRepo) Delete(_ context.Context, _ string) error    { return nil }

// --- helpers ---

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

// --- tests ---

func TestSitemapHandler_Empty(t *testing.T) {
	h := shophttp.NewSitemapHandler("https://example.com",
		&stubSitemapProductRepo{},
		&stubSitemapCategoryRepo{},
		&stubSitemapPageRepo{},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sitemap.xml", nil)
	h.Serve().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("Content-Type = %q, want application/xml", ct)
	}

	var urlset sitemapURLSet
	if err := xml.Unmarshal(rec.Body.Bytes(), &urlset); err != nil {
		t.Fatalf("XML parse: %v", err)
	}
	if len(urlset.URLs) != 0 {
		t.Errorf("urls = %d, want 0", len(urlset.URLs))
	}
}

func TestSitemapHandler_ProductsCategoriesPages(t *testing.T) {
	now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	products := []catalog.Product{
		{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive, UpdatedAt: now},
		{ID: "p2", Name: "Draft", Slug: "draft", Status: catalog.StatusDraft, UpdatedAt: now},
	}
	categories := []catalog.Category{
		{ID: "c1", Name: "Tools", Slug: "tools", UpdatedAt: now},
	}
	page := cms.NewPageFromDB("pg1", "about", "About", "", true, now, now)

	h := shophttp.NewSitemapHandler("https://shop.test",
		&stubSitemapProductRepo{products: products},
		&stubSitemapCategoryRepo{categories: categories},
		&stubSitemapPageRepo{pages: []*cms.Page{page}},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sitemap.xml", nil)
	h.Serve().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var urlset sitemapURLSet
	if err := xml.Unmarshal(rec.Body.Bytes(), &urlset); err != nil {
		t.Fatalf("XML parse: %v", err)
	}
	// 1 active product + 1 category + 1 active page = 3
	if len(urlset.URLs) != 3 {
		t.Fatalf("urls = %d, want 3", len(urlset.URLs))
	}

	if urlset.URLs[0].Loc != "https://shop.test/products/widget" {
		t.Errorf("url[0] = %q, want product URL", urlset.URLs[0].Loc)
	}
	if urlset.URLs[0].LastMod != "2025-06-15" {
		t.Errorf("lastmod = %q, want 2025-06-15", urlset.URLs[0].LastMod)
	}
	if urlset.URLs[1].Loc != "https://shop.test/categories/tools" {
		t.Errorf("url[1] = %q, want category URL", urlset.URLs[1].Loc)
	}
	if urlset.URLs[2].Loc != "https://shop.test/pages/about" {
		t.Errorf("url[2] = %q, want page URL", urlset.URLs[2].Loc)
	}
}

func TestSitemapHandler_InactivePageExcluded(t *testing.T) {
	now := time.Now().UTC()
	inactivePage := cms.NewPageFromDB("pg1", "hidden", "Hidden", "", false, now, now)

	h := shophttp.NewSitemapHandler("https://shop.test",
		&stubSitemapProductRepo{},
		&stubSitemapCategoryRepo{},
		&stubSitemapPageRepo{pages: []*cms.Page{inactivePage}},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sitemap.xml", nil)
	h.Serve().ServeHTTP(rec, req)

	var urlset sitemapURLSet
	if err := xml.Unmarshal(rec.Body.Bytes(), &urlset); err != nil {
		t.Fatalf("XML parse: %v", err)
	}
	if len(urlset.URLs) != 0 {
		t.Errorf("urls = %d, want 0 (inactive page excluded)", len(urlset.URLs))
	}
}

func TestSitemapHandler_ProductRepoError(t *testing.T) {
	h := shophttp.NewSitemapHandler("https://shop.test",
		&stubSitemapProductRepo{err: context.DeadlineExceeded},
		&stubSitemapCategoryRepo{},
		&stubSitemapPageRepo{},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sitemap.xml", nil)
	h.Serve().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
