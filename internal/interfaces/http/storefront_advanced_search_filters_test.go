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

func TestStorefrontHandler_Search_AdvancedSearchAttributeFilter(t *testing.T) {
	t.Parallel()

	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return nil, nil
	}}
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if query.Filters["attr_brand"] != "acme" {
			t.Fatalf("attr_brand filter = %v, want acme", query.Filters["attr_brand"])
		}
		if len(query.FacetAttributes) != 1 || query.FacetAttributes[0] != "brand" {
			t.Fatalf("FacetAttributes = %v, want [brand]", query.FacetAttributes)
		}
		return search.SearchResult{
			Products: []search.Product{{Name: "Acme Shirt", Slug: "acme-shirt"}},
			Total:    1,
			Facets: map[string][]search.FacetValue{
				"brand": {{Value: "acme", Count: 1}},
			},
		}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, searchEngine).
		WithAdvancedSearchAttributes(&mockAdvancedSearchAttributeLister{attrs: []catalog.Attribute{
			{Code: "brand", Label: "Brand", Type: catalog.AttributeTypeSelect, UseInAdvancedSearch: true},
		}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/search?q=shirt&attr_brand=acme", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Acme Shirt") {
		t.Fatalf("body missing filtered product: %s", body)
	}
	if !strings.Contains(body, `attr_brand=acme`) {
		t.Fatalf("body missing advanced search facet link: %s", body)
	}
}

func TestStorefrontHandler_Products_IgnoresAdvancedSearchOnlyAttribute(t *testing.T) {
	t.Parallel()

	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return nil, nil
	}}
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if _, ok := query.Filters["attr_brand"]; ok {
			t.Fatalf("unexpected attr_brand on /products: %v", query.Filters)
		}
		return search.SearchResult{
			Products: []search.Product{{Name: "Shirt", Slug: "shirt"}},
			Total:    1,
		}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, searchEngine).
		WithAdvancedSearchAttributes(&mockAdvancedSearchAttributeLister{attrs: []catalog.Attribute{
			{Code: "brand", Label: "Brand", Type: catalog.AttributeTypeSelect, UseInAdvancedSearch: true},
		}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products?attr_brand=acme", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
}
