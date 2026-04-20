package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- mock category repository ---

type mockCategoryRepo struct {
	findByIDFn     func(ctx context.Context, id string) (*catalog.Category, error)
	findBySlugFn   func(ctx context.Context, slug string) (*catalog.Category, error)
	findByParentFn func(ctx context.Context, parentID *string) ([]catalog.Category, error)
	findAllFn      func(ctx context.Context) ([]catalog.Category, error)
	createFn       func(ctx context.Context, c *catalog.Category) error
	updateFn       func(ctx context.Context, c *catalog.Category) error
}

func (m *mockCategoryRepo) FindByID(ctx context.Context, id string) (*catalog.Category, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockCategoryRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Category, error) {
	if m.findBySlugFn != nil {
		return m.findBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockCategoryRepo) FindByParentID(ctx context.Context, parentID *string) ([]catalog.Category, error) {
	if m.findByParentFn != nil {
		return m.findByParentFn(ctx, parentID)
	}
	return nil, nil
}

func (m *mockCategoryRepo) FindAll(ctx context.Context) ([]catalog.Category, error) {
	if m.findAllFn != nil {
		return m.findAllFn(ctx)
	}
	return nil, nil
}

func (m *mockCategoryRepo) Create(ctx context.Context, c *catalog.Category) error {
	if m.createFn != nil {
		return m.createFn(ctx, c)
	}
	return nil
}

func (m *mockCategoryRepo) Update(ctx context.Context, c *catalog.Category) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, c)
	}
	return nil
}

// --- mock product repository (category-scoped) ---

type mockCatProductRepo struct {
	findByIDFn         func(ctx context.Context, id string) (*catalog.Product, error)
	findBySlugFn       func(ctx context.Context, slug string) (*catalog.Product, error)
	listFn             func(ctx context.Context, offset, limit int) ([]catalog.Product, error)
	findByCategoryIDFn func(ctx context.Context, categoryID string, offset, limit int) ([]catalog.Product, error)
	createFn           func(ctx context.Context, p *catalog.Product) error
	updateFn           func(ctx context.Context, p *catalog.Product) error
}

func (m *mockCatProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockCatProductRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	if m.findBySlugFn != nil {
		return m.findBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockCatProductRepo) List(ctx context.Context, offset, limit int) ([]catalog.Product, error) {
	if m.listFn != nil {
		return m.listFn(ctx, offset, limit)
	}
	return nil, nil
}

func (m *mockCatProductRepo) FindByCategoryID(ctx context.Context, categoryID string, offset, limit int) ([]catalog.Product, error) {
	if m.findByCategoryIDFn != nil {
		return m.findByCategoryIDFn(ctx, categoryID, offset, limit)
	}
	return nil, nil
}

func (m *mockCatProductRepo) Create(ctx context.Context, p *catalog.Product) error {
	if m.createFn != nil {
		return m.createFn(ctx, p)
	}
	return nil
}

func (m *mockCatProductRepo) Update(ctx context.Context, p *catalog.Product) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, p)
	}
	return nil
}

// --- router helper ---

func newCategoryRouter(h *shophttp.CategoryHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/categories", h.Tree())
	mux.HandleFunc("GET /api/v1/categories/{id}", h.Get())
	mux.HandleFunc("GET /api/v1/categories/{id}/products", h.Products())
	return mux
}

// --- tests ---

func TestCategoryHandler_Tree_OK(t *testing.T) {
	cats := &mockCategoryRepo{
		findAllFn: func(_ context.Context) ([]catalog.Category, error) {
			parentID := "cat-1"
			return []catalog.Category{
				{ID: "cat-1", Name: "Electronics", Slug: "electronics", Position: 1},
				{ID: "cat-2", ParentID: &parentID, Name: "Phones", Slug: "phones", Position: 1},
			}, nil
		},
	}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories")
	if err != nil {
		t.Fatalf("GET categories: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var envelope struct {
		Data struct {
			Categories []map[string]interface{} `json:"categories"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}

	tree := envelope.Data.Categories
	if len(tree) != 1 {
		t.Fatalf("root count = %d, want 1", len(tree))
	}
	if tree[0]["name"] != "Electronics" {
		t.Errorf("root name = %v, want Electronics", tree[0]["name"])
	}
	children, ok := tree[0]["children"].([]interface{})
	if !ok || len(children) != 1 {
		t.Fatalf("children count = %v, want 1", tree[0]["children"])
	}
}

func TestCategoryHandler_Tree_RepoError(t *testing.T) {
	cats := &mockCategoryRepo{
		findAllFn: func(_ context.Context) ([]catalog.Category, error) {
			return nil, errors.New("db down")
		},
	}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories")
	if err != nil {
		t.Fatalf("GET categories: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected error status, got 200")
	}
}

func TestCategoryHandler_Get_OK(t *testing.T) {
	cat := &catalog.Category{ID: "cat-1", Name: "Books", Slug: "books"}
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			if id == "cat-1" {
				return cat, nil
			}
			return nil, nil
		},
	}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories/cat-1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var envelope struct {
		Data struct {
			Category json.RawMessage `json:"category"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(envelope.Data.Category) == 0 {
		t.Fatal("response missing 'category' in data")
	}
}

func TestCategoryHandler_Get_NotFound(t *testing.T) {
	cats := &mockCategoryRepo{}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories/missing-id")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCategoryHandler_Products_OK(t *testing.T) {
	cat := &catalog.Category{ID: "cat-1", Name: "Books", Slug: "books"}
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			if id == "cat-1" {
				return cat, nil
			}
			return nil, nil
		},
	}
	prods := &mockCatProductRepo{
		findByCategoryIDFn: func(_ context.Context, catID string, offset, limit int) ([]catalog.Product, error) {
			return []catalog.Product{
				{ID: "p-1", Name: "Go Book", Slug: "go-book"},
			}, nil
		},
	}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories/cat-1/products")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var envelope struct {
		Data struct {
			Products json.RawMessage `json:"products"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(envelope.Data.Products) == 0 {
		t.Fatal("response missing 'products' in data")
	}
}

func TestCategoryHandler_Products_CategoryNotFound(t *testing.T) {
	cats := &mockCategoryRepo{}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories/missing-id/products")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCategoryHandler_Tree_Empty(t *testing.T) {
	cats := &mockCategoryRepo{
		findAllFn: func(_ context.Context) ([]catalog.Category, error) {
			return nil, nil
		},
	}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories")
	if err != nil {
		t.Fatalf("GET categories: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
