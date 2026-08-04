package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/search"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type mockLayeredNavAttributeLister struct {
	attrs []catalog.Attribute
	err   error
}

func (m *mockLayeredNavAttributeLister) ListLayeredNavAttributes(context.Context) ([]catalog.Attribute, error) {
	return m.attrs, m.err
}

func TestStorefrontHandler_Products_AttributeFacetLink(t *testing.T) {
	t.Parallel()

	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return nil, nil
	}}
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if len(query.FacetAttributes) != 1 || query.FacetAttributes[0] != "color" {
			t.Fatalf("FacetAttributes = %v, want [color]", query.FacetAttributes)
		}
		return search.SearchResult{
			Products: []search.Product{{Name: "Shirt", Slug: "shirt"}},
			Total:    1,
			Facets: map[string][]search.FacetValue{
				"color": {{Value: "red", Count: 2}, {Value: "blue", Count: 1}},
			},
		}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, searchEngine).
		WithLayeredNavAttributes(&mockLayeredNavAttributeLister{attrs: []catalog.Attribute{
			{Code: "color", Label: "Color", Type: catalog.AttributeTypeSelect, UseInLayeredNav: true},
		}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `attr_color=red`) {
		t.Fatalf("body missing attribute facet link: %s", body)
	}
	if !strings.Contains(body, "blue") {
		t.Fatalf("body missing second facet value: %s", body)
	}
}

func TestStorefrontHandler_Products_AttributeFilterParam(t *testing.T) {
	t.Parallel()

	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return nil, nil
	}}
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if query.Filters["attr_color"] != "red" {
			t.Fatalf("attr_color filter = %v, want red", query.Filters["attr_color"])
		}
		return search.SearchResult{
			Products: []search.Product{{Name: "Red Shirt", Slug: "red-shirt"}},
			Total:    1,
			Facets: map[string][]search.FacetValue{
				"color": {{Value: "red", Count: 1}},
			},
		}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, searchEngine).
		WithLayeredNavAttributes(&mockLayeredNavAttributeLister{attrs: []catalog.Attribute{
			{Code: "color", Label: "Color", Type: catalog.AttributeTypeSelect, UseInLayeredNav: true},
		}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products?attr_color=red", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Red Shirt") {
		t.Fatalf("body missing filtered product: %s", body)
	}
	if !strings.Contains(body, `data-selected="true"`) {
		t.Fatalf("body missing selected attribute facet: %s", body)
	}
}
