package graphql_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	cgraphql "github.com/akarso/shopanda/plugins/core/graphql"
)

type stubProductRepo struct {
	findByIDFn         func(ctx context.Context, id string) (*catalog.Product, error)
	findBySlugFn       func(ctx context.Context, slug string) (*catalog.Product, error)
	listFn             func(ctx context.Context, offset, limit int) ([]catalog.Product, error)
	findByCategoryIDFn func(ctx context.Context, categoryID string, offset, limit int) ([]catalog.Product, error)
}

func (s *stubProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	if s.findByIDFn != nil {
		return s.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (s *stubProductRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	if s.findBySlugFn != nil {
		return s.findBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (s *stubProductRepo) List(ctx context.Context, offset, limit int) ([]catalog.Product, error) {
	if s.listFn != nil {
		return s.listFn(ctx, offset, limit)
	}
	return nil, nil
}

func (s *stubProductRepo) FindByCategoryID(ctx context.Context, categoryID string, offset, limit int) ([]catalog.Product, error) {
	if s.findByCategoryIDFn != nil {
		return s.findByCategoryIDFn(ctx, categoryID, offset, limit)
	}
	return nil, nil
}

func (s *stubProductRepo) Create(context.Context, *catalog.Product) error  { return nil }
func (s *stubProductRepo) Update(context.Context, *catalog.Product) error  { return nil }

type stubCategoryRepo struct {
	findByIDFn func(ctx context.Context, id string) (*catalog.Category, error)
	findAllFn  func(ctx context.Context) ([]catalog.Category, error)
}

func (s *stubCategoryRepo) FindByID(ctx context.Context, id string) (*catalog.Category, error) {
	if s.findByIDFn != nil {
		return s.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (s *stubCategoryRepo) FindBySlug(context.Context, string) (*catalog.Category, error) {
	return nil, nil
}

func (s *stubCategoryRepo) FindByParentID(context.Context, *string) ([]catalog.Category, error) {
	return nil, nil
}

func (s *stubCategoryRepo) FindAll(ctx context.Context) ([]catalog.Category, error) {
	if s.findAllFn != nil {
		return s.findAllFn(ctx)
	}
	return nil, nil
}

func (s *stubCategoryRepo) Create(context.Context, *catalog.Category) error { return nil }
func (s *stubCategoryRepo) Update(context.Context, *catalog.Category) error { return nil }
func (s *stubCategoryRepo) Delete(context.Context, string) error            { return nil }

func testResolver(t *testing.T) *cgraphql.Resolver {
	t.Helper()
	product, err := catalog.NewProduct("prod-1", "Widget", "widget")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	product.Description = "A widget"

	category, err := catalog.NewCategory("cat-1", "Gadgets", "gadgets")
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}

	resolver, err := cgraphql.NewResolver(
		&stubProductRepo{
			findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
				if id == product.ID {
					return &product, nil
				}
				return nil, nil
			},
			listFn: func(_ context.Context, _, _ int) ([]catalog.Product, error) {
				return []catalog.Product{product}, nil
			},
		},
		&stubCategoryRepo{
			findAllFn: func(context.Context) ([]catalog.Category, error) {
				return []catalog.Category{category}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return resolver
}

func TestHandler_ProductQuery(t *testing.T) {
	schema, err := cgraphql.NewSchema(testResolver(t))
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	h := cgraphql.NewHandler(schema)

	body := `{"query":"{ product(id: \"prod-1\") { id name slug status } }"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data struct {
			Product struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Slug   string `json:"slug"`
				Status string `json:"status"`
			} `json:"product"`
		} `json:"data"`
		Errors []map[string]interface{} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("graphql errors: %v", resp.Errors)
	}
	if resp.Data.Product.ID != "prod-1" || resp.Data.Product.Name != "Widget" || resp.Data.Product.Status != "draft" {
		t.Fatalf("product = %+v", resp.Data.Product)
	}
}

func TestHandler_RequiresQuery(t *testing.T) {
	schema, err := cgraphql.NewSchema(testResolver(t))
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	h := cgraphql.NewHandler(schema)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_RejectsNonPost(t *testing.T) {
	schema, err := cgraphql.NewSchema(testResolver(t))
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	h := cgraphql.NewHandler(schema)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/graphql", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
