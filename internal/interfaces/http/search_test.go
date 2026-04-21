package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- mock search engine ---

type mockSearchEngine struct {
	searchFn  func(ctx context.Context, query search.SearchQuery) (search.SearchResult, error)
	suggestFn func(ctx context.Context, prefix string, limit int) ([]search.Suggestion, error)
}

func (m *mockSearchEngine) Name() string { return "mock" }

func (m *mockSearchEngine) IndexProduct(_ context.Context, _ search.Product) error { return nil }

func (m *mockSearchEngine) RemoveProduct(_ context.Context, _ string) error { return nil }

func (m *mockSearchEngine) Search(ctx context.Context, query search.SearchQuery) (search.SearchResult, error) {
	return m.searchFn(ctx, query)
}

func (m *mockSearchEngine) Suggest(ctx context.Context, prefix string, limit int) ([]search.Suggestion, error) {
	if m.suggestFn != nil {
		return m.suggestFn(ctx, prefix, limit)
	}
	return nil, nil
}

// --- helpers ---

func newSearchRouter(h *shophttp.SearchHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/search", h.Search())
	mux.HandleFunc("GET /api/v1/search/suggest", h.Suggest())
	return mux
}

func parseSearchBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return body
}

// --- tests ---

func TestSearchHandler_OK(t *testing.T) {
	engine := &mockSearchEngine{
		searchFn: func(_ context.Context, q search.SearchQuery) (search.SearchResult, error) {
			if q.Text != "shoes" {
				t.Fatalf("text = %q, want %q", q.Text, "shoes")
			}
			return search.SearchResult{
				Products: []search.Product{
					{ID: "p1", Name: "Running Shoes", Slug: "running-shoes", Description: "Great", Attributes: map[string]interface{}{}},
				},
				Total: 1,
				Facets: map[string][]search.FacetValue{
					"category": {{Value: "footwear", Count: 1}},
				},
			}, nil
		},
	}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?q=shoes", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := parseSearchBody(t, rec)
	data := body["data"].(map[string]interface{})
	products := data["products"].([]interface{})
	if len(products) != 1 {
		t.Fatalf("products len = %d, want 1", len(products))
	}
	p := products[0].(map[string]interface{})
	if p["id"] != "p1" {
		t.Errorf("id = %v, want p1", p["id"])
	}
	total := data["total"].(float64)
	if total != 1 {
		t.Errorf("total = %v, want 1", total)
	}
	facets := data["facets"].(map[string]interface{})
	catFacets := facets["category"].([]interface{})
	if len(catFacets) != 1 {
		t.Fatalf("category facets len = %d, want 1", len(catFacets))
	}
}

func TestSearchHandler_EmptyResults(t *testing.T) {
	engine := &mockSearchEngine{
		searchFn: func(_ context.Context, _ search.SearchQuery) (search.SearchResult, error) {
			return search.SearchResult{
				Products: []search.Product{},
				Facets:   map[string][]search.FacetValue{},
			}, nil
		},
	}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := parseSearchBody(t, rec)
	data := body["data"].(map[string]interface{})
	products := data["products"].([]interface{})
	if len(products) != 0 {
		t.Errorf("products len = %d, want 0", len(products))
	}
}

func TestSearchHandler_WithPagination(t *testing.T) {
	var capturedQuery search.SearchQuery
	engine := &mockSearchEngine{
		searchFn: func(_ context.Context, q search.SearchQuery) (search.SearchResult, error) {
			capturedQuery = q
			return search.SearchResult{Products: []search.Product{}, Facets: map[string][]search.FacetValue{}}, nil
		},
	}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?q=test&limit=10&offset=5", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturedQuery.Limit != 10 {
		t.Errorf("limit = %d, want 10", capturedQuery.Limit)
	}
	if capturedQuery.Offset != 5 {
		t.Errorf("offset = %d, want 5", capturedQuery.Offset)
	}
}

func TestSearchHandler_WithSort(t *testing.T) {
	var capturedQuery search.SearchQuery
	engine := &mockSearchEngine{
		searchFn: func(_ context.Context, q search.SearchQuery) (search.SearchResult, error) {
			capturedQuery = q
			return search.SearchResult{Products: []search.Product{}, Facets: map[string][]search.FacetValue{}}, nil
		},
	}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?q=test&sort=name", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturedQuery.Sort != "name" {
		t.Errorf("sort = %q, want %q", capturedQuery.Sort, "name")
	}
}

func TestSearchHandler_WithCategoryFilter(t *testing.T) {
	var capturedQuery search.SearchQuery
	engine := &mockSearchEngine{
		searchFn: func(_ context.Context, q search.SearchQuery) (search.SearchResult, error) {
			capturedQuery = q
			return search.SearchResult{Products: []search.Product{}, Facets: map[string][]search.FacetValue{}}, nil
		},
	}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?q=shoes&category=footwear", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	cat, ok := capturedQuery.Filters["category"]
	if !ok {
		t.Fatal("expected category filter")
	}
	if cat != "footwear" {
		t.Errorf("category = %v, want footwear", cat)
	}
}

func TestSearchHandler_UsesStoreContext(t *testing.T) {
	var capturedQuery search.SearchQuery
	engine := &mockSearchEngine{
		searchFn: func(_ context.Context, q search.SearchQuery) (search.SearchResult, error) {
			capturedQuery = q
			return search.SearchResult{Products: []search.Product{}, Facets: map[string][]search.FacetValue{}}, nil
		},
	}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?q=test", nil)
	ctx := store.WithStore(req.Context(), &store.Store{ID: "store-1", Currency: "EUR"})
	newSearchRouter(h).ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturedQuery.StoreID != "store-1" {
		t.Errorf("StoreID = %q, want %q", capturedQuery.StoreID, "store-1")
	}
	if capturedQuery.Currency != "EUR" {
		t.Errorf("Currency = %q, want %q", capturedQuery.Currency, "EUR")
	}
}

func TestSearchHandler_InvalidLimit(t *testing.T) {
	engine := &mockSearchEngine{}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?limit=abc", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestSearchHandler_NegativeLimit(t *testing.T) {
	engine := &mockSearchEngine{}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?limit=-1", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestSearchHandler_InvalidOffset(t *testing.T) {
	engine := &mockSearchEngine{}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?offset=abc", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestSearchHandler_NegativeOffset(t *testing.T) {
	engine := &mockSearchEngine{}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?offset=-1", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestSearchHandler_EngineError(t *testing.T) {
	engine := &mockSearchEngine{
		searchFn: func(_ context.Context, _ search.SearchQuery) (search.SearchResult, error) {
			return search.SearchResult{}, errors.New("db down")
		},
	}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?q=test", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestSearchHandler_ValidationError(t *testing.T) {
	engine := &mockSearchEngine{
		searchFn: func(_ context.Context, _ search.SearchQuery) (search.SearchResult, error) {
			return search.SearchResult{}, apperror.Validation("offset must not be negative")
		},
	}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search?q=test", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

// --- Suggest handler tests ---

func TestSuggestHandler_OK(t *testing.T) {
	engine := &mockSearchEngine{
		suggestFn: func(_ context.Context, prefix string, limit int) ([]search.Suggestion, error) {
			if prefix != "sne" {
				t.Errorf("prefix = %q, want sne", prefix)
			}
			if limit != 5 {
				t.Errorf("limit = %d, want 5", limit)
			}
			return []search.Suggestion{
				{Text: "Sneakers", Type: "product", URL: "/products/sneakers"},
			}, nil
		},
	}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search/suggest?q=sne&limit=5", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := parseSearchBody(t, rec)
	data := body["data"].(map[string]interface{})
	suggestions := data["suggestions"].([]interface{})
	if len(suggestions) != 1 {
		t.Fatalf("suggestions len = %d, want 1", len(suggestions))
	}
	s := suggestions[0].(map[string]interface{})
	if s["text"] != "Sneakers" {
		t.Errorf("text = %v, want Sneakers", s["text"])
	}
	if s["type"] != "product" {
		t.Errorf("type = %v, want product", s["type"])
	}
	if s["url"] != "/products/sneakers" {
		t.Errorf("url = %v, want /products/sneakers", s["url"])
	}
}

func TestSuggestHandler_EmptyQuery(t *testing.T) {
	engine := &mockSearchEngine{}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search/suggest", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := parseSearchBody(t, rec)
	data := body["data"].(map[string]interface{})
	suggestions := data["suggestions"].([]interface{})
	if len(suggestions) != 0 {
		t.Errorf("suggestions len = %d, want 0", len(suggestions))
	}
}

func TestSuggestHandler_InvalidLimit(t *testing.T) {
	engine := &mockSearchEngine{}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search/suggest?q=test&limit=abc", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestSuggestHandler_ZeroLimit(t *testing.T) {
	engine := &mockSearchEngine{}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search/suggest?q=test&limit=0", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestSuggestHandler_EngineError(t *testing.T) {
	engine := &mockSearchEngine{
		suggestFn: func(_ context.Context, _ string, _ int) ([]search.Suggestion, error) {
			return nil, errors.New("db down")
		},
	}
	h := shophttp.NewSearchHandler(engine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/search/suggest?q=test", nil)
	newSearchRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
