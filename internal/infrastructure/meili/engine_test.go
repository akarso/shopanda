package meili

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/search"
)

// --- mock meiliAPI ---

type mockAPI struct {
	addDocs   [][]document
	addErr    error
	deleteIDs []string
	deleteErr error

	searchReq  searchRequest
	searchResp searchResponse
	searchErr  error

	settingsReq indexSettings
	settingsErr error

	// taskUID is returned by write methods; taskStatus is returned by getTask.
	taskUID    int64
	taskStatus string
	taskErr    error
}

func (m *mockAPI) addDocuments(_ context.Context, docs []document) (int64, error) {
	if m.addErr != nil {
		return 0, m.addErr
	}
	m.addDocs = append(m.addDocs, docs)
	return m.taskUID, nil
}

func (m *mockAPI) deleteDocument(_ context.Context, id string) (int64, error) {
	if m.deleteErr != nil {
		return 0, m.deleteErr
	}
	m.deleteIDs = append(m.deleteIDs, id)
	return m.taskUID, nil
}

func (m *mockAPI) search(_ context.Context, req searchRequest) (searchResponse, error) {
	m.searchReq = req
	if m.searchErr != nil {
		return searchResponse{}, m.searchErr
	}
	return m.searchResp, nil
}

func (m *mockAPI) updateSettings(_ context.Context, s indexSettings) (int64, error) {
	m.settingsReq = s
	return m.taskUID, m.settingsErr
}

func (m *mockAPI) getTask(_ context.Context, _ int64) (taskInfo, error) {
	if m.taskErr != nil {
		return taskInfo{}, m.taskErr
	}
	status := m.taskStatus
	if status == "" {
		status = "succeeded"
	}
	return taskInfo{TaskUID: m.taskUID, Status: status}, nil
}

// --- tests ---

func TestName(t *testing.T) {
	e := newWithAPI(&mockAPI{}, "products")
	if e.Name() != "meilisearch" {
		t.Errorf("Name() = %q, want meilisearch", e.Name())
	}
}

func TestIndexProduct(t *testing.T) {
	mock := &mockAPI{}
	e := newWithAPI(mock, "products")

	p := search.Product{ID: "p-1", Name: "Shoes", Slug: "shoes", Description: "Running shoes"}
	if err := e.IndexProduct(context.Background(), p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.addDocs) != 1 || len(mock.addDocs[0]) != 1 {
		t.Fatalf("expected 1 doc batch, got %d", len(mock.addDocs))
	}
	doc := mock.addDocs[0][0]
	if doc.ID != "p-1" || doc.Name != "Shoes" || doc.Slug != "shoes" {
		t.Errorf("doc = %+v, want id=p-1 name=Shoes slug=shoes", doc)
	}
}

func TestIndexProduct_Error(t *testing.T) {
	mock := &mockAPI{addErr: errors.New("connection refused")}
	e := newWithAPI(mock, "products")

	err := e.IndexProduct(context.Background(), search.Product{ID: "p-1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %q, want 'connection refused'", err.Error())
	}
}

func TestRemoveProduct(t *testing.T) {
	mock := &mockAPI{}
	e := newWithAPI(mock, "products")

	if err := e.RemoveProduct(context.Background(), "p-42"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deleteIDs) != 1 || mock.deleteIDs[0] != "p-42" {
		t.Errorf("deleteIDs = %v, want [p-42]", mock.deleteIDs)
	}
}

func TestRemoveProduct_Error(t *testing.T) {
	mock := &mockAPI{deleteErr: errors.New("not found")}
	e := newWithAPI(mock, "products")

	err := e.RemoveProduct(context.Background(), "p-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err.Error())
	}
}

func TestSearch(t *testing.T) {
	hit, _ := json.Marshal(document{ID: "p-1", Name: "Sneakers", Slug: "sneakers", Description: "White sneakers"})
	mock := &mockAPI{
		searchResp: searchResponse{
			Hits:               []json.RawMessage{hit},
			EstimatedTotalHits: 42,
			FacetDistribution: map[string]map[string]int{
				"category_id": {"cat-1": 10, "cat-2": 5},
			},
		},
	}
	e := newWithAPI(mock, "products")

	result, err := e.Search(context.Background(), search.SearchQuery{Text: "sneak", Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request forwarding.
	if mock.searchReq.Q != "sneak" {
		t.Errorf("searchReq.Q = %q, want sneak", mock.searchReq.Q)
	}
	if mock.searchReq.Limit != 10 {
		t.Errorf("searchReq.Limit = %d, want 10", mock.searchReq.Limit)
	}

	// Verify result mapping.
	if result.Total != 42 {
		t.Errorf("Total = %d, want 42", result.Total)
	}
	if len(result.Products) != 1 || result.Products[0].ID != "p-1" {
		t.Errorf("Products = %+v, want 1 product with id=p-1", result.Products)
	}
	if len(result.Facets["category_id"]) != 2 {
		t.Errorf("Facets[category_id] = %+v, want 2 values", result.Facets["category_id"])
	}
}

func TestSearch_Error(t *testing.T) {
	mock := &mockAPI{searchErr: errors.New("timeout")}
	e := newWithAPI(mock, "products")

	_, err := e.Search(context.Background(), search.SearchQuery{Text: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error = %q, want 'timeout'", err.Error())
	}
}

func TestSearch_InvalidQuery(t *testing.T) {
	mock := &mockAPI{}
	e := newWithAPI(mock, "products")

	_, err := e.Search(context.Background(), search.SearchQuery{Offset: -1})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestBuildSearchRequest_Filters(t *testing.T) {
	q := search.SearchQuery{
		Text: "shoes",
		Filters: map[string]interface{}{
			"category":  "cat-1",
			"price_min": 2000,
			"price_max": 5000,
		},
		Sort:  "-price",
		Limit: 20,
	}

	req := buildSearchRequest(q)

	if req.Q != "shoes" {
		t.Errorf("Q = %q, want shoes", req.Q)
	}
	if !strings.Contains(req.Filter, "category_id = ") {
		t.Errorf("Filter = %q, want category filter", req.Filter)
	}
	if !strings.Contains(req.Filter, "price >= 2000") {
		t.Errorf("Filter = %q, want price_min filter", req.Filter)
	}
	if !strings.Contains(req.Filter, "price <= 5000") {
		t.Errorf("Filter = %q, want price_max filter", req.Filter)
	}
	if len(req.Sort) != 1 || req.Sort[0] != "price:desc" {
		t.Errorf("Sort = %v, want [price:desc]", req.Sort)
	}
}

func TestBuildSearchRequest_SortAsc(t *testing.T) {
	req := buildSearchRequest(search.SearchQuery{Sort: "name", Limit: 10})
	if len(req.Sort) != 1 || req.Sort[0] != "name:asc" {
		t.Errorf("Sort = %v, want [name:asc]", req.Sort)
	}
}

func TestBuildSearchRequest_NoSort(t *testing.T) {
	req := buildSearchRequest(search.SearchQuery{Limit: 10})
	if len(req.Sort) != 0 {
		t.Errorf("Sort = %v, want empty", req.Sort)
	}
}

func TestMapSearchResponse_Empty(t *testing.T) {
	resp := searchResponse{
		Hits:               nil,
		EstimatedTotalHits: 0,
	}
	result := mapSearchResponse(resp)
	if len(result.Products) != 0 {
		t.Errorf("Products = %+v, want empty", result.Products)
	}
	if result.Total != 0 {
		t.Errorf("Total = %d, want 0", result.Total)
	}
}

func TestProductToDoc(t *testing.T) {
	p := search.Product{
		ID:          "p-99",
		Name:        "Hat",
		Slug:        "hat",
		Description: "A nice hat",
		CategoryID:  "cat-5",
		Price:       1999,
		InStock:     true,
		Attributes:  map[string]interface{}{"color": "red"},
	}
	doc := productToDoc(p)
	if doc.ID != "p-99" || doc.Name != "Hat" || doc.Slug != "hat" {
		t.Errorf("doc = %+v", doc)
	}
	if doc.CategoryID != "cat-5" {
		t.Errorf("CategoryID = %q, want cat-5", doc.CategoryID)
	}
	if doc.Price != 1999 {
		t.Errorf("Price = %d, want 1999", doc.Price)
	}
	if !doc.InStock {
		t.Error("InStock = false, want true")
	}
	if doc.Attributes["color"] != "red" {
		t.Errorf("Attributes = %+v, want color=red", doc.Attributes)
	}
}

func TestWaitForTask_Failed(t *testing.T) {
	mock := &mockAPI{taskStatus: "failed"}
	e := newWithAPI(mock, "products")
	err := e.waitForTask(context.Background(), 42)
	if err == nil {
		t.Fatal("expected error for failed task")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error = %q, want 'failed'", err.Error())
	}
}

func TestWaitForTask_Succeeded(t *testing.T) {
	mock := &mockAPI{taskStatus: "succeeded"}
	e := newWithAPI(mock, "products")
	if err := e.waitForTask(context.Background(), 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultSettings(t *testing.T) {
	s := defaultSettings()
	if len(s.SearchableAttributes) == 0 {
		t.Error("SearchableAttributes is empty")
	}
	if len(s.FilterableAttributes) == 0 {
		t.Error("FilterableAttributes is empty")
	}
	if len(s.SortableAttributes) == 0 {
		t.Error("SortableAttributes is empty")
	}
	if len(s.DisplayedAttributes) != 1 || s.DisplayedAttributes[0] != "*" {
		t.Errorf("DisplayedAttributes = %v, want [*]", s.DisplayedAttributes)
	}
}

func TestSuggest(t *testing.T) {
	hit, _ := json.Marshal(document{ID: "p-1", Name: "Sneakers", Slug: "sneakers"})
	mock := &mockAPI{
		searchResp: searchResponse{
			Hits:               []json.RawMessage{hit},
			EstimatedTotalHits: 1,
		},
	}
	e := newWithAPI(mock, "products")

	suggestions, err := e.Suggest(context.Background(), "sne", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("got %d suggestions, want 1", len(suggestions))
	}
	if suggestions[0].Text != "Sneakers" {
		t.Errorf("Text = %q, want Sneakers", suggestions[0].Text)
	}
	if suggestions[0].Type != "product" {
		t.Errorf("Type = %q, want product", suggestions[0].Type)
	}
	if suggestions[0].URL != "/products/sneakers" {
		t.Errorf("URL = %q, want /products/sneakers", suggestions[0].URL)
	}
	// Verify search request had correct limit.
	if mock.searchReq.Limit != 5 {
		t.Errorf("searchReq.Limit = %d, want 5", mock.searchReq.Limit)
	}
}

func TestSuggest_EmptyPrefix(t *testing.T) {
	mock := &mockAPI{}
	e := newWithAPI(mock, "products")

	suggestions, err := e.Suggest(context.Background(), "", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if suggestions != nil {
		t.Errorf("expected nil for empty prefix, got %v", suggestions)
	}
}

func TestSuggest_GracefulDegradation(t *testing.T) {
	mock := &mockAPI{searchErr: errors.New("connection refused")}
	e := newWithAPI(mock, "products")

	suggestions, err := e.Suggest(context.Background(), "sne", 5)
	if err != nil {
		t.Fatalf("expected nil error on degradation, got %v", err)
	}
	if suggestions != nil {
		t.Errorf("expected nil suggestions on degradation, got %v", suggestions)
	}
}

func TestClampSuggestLimit(t *testing.T) {
	tests := []struct {
		input, want int
	}{
		{0, 5},   // default
		{-1, 5},  // default
		{3, 3},   // passthrough
		{10, 10}, // max
		{15, 10}, // clamped
	}
	for _, tt := range tests {
		got := clampSuggestLimit(tt.input)
		if got != tt.want {
			t.Errorf("clampSuggestLimit(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
