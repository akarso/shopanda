package search_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/search"
)

func TestSearchQuery_Validate(t *testing.T) {
	tests := []struct {
		name    string
		query   search.SearchQuery
		wantErr bool
	}{
		{
			name:    "valid empty query",
			query:   search.SearchQuery{},
			wantErr: false,
		},
		{
			name:    "valid full query",
			query:   search.SearchQuery{Text: "shoes", Sort: "price", Limit: 10, Offset: 0},
			wantErr: false,
		},
		{
			name:    "negative offset",
			query:   search.SearchQuery{Offset: -1},
			wantErr: true,
		},
		{
			name:    "negative limit",
			query:   search.SearchQuery{Limit: -1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.query.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSearchQuery_EffectiveLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "zero uses default", limit: 0, want: 20},
		{name: "negative uses default", limit: -5, want: 20},
		{name: "within range", limit: 50, want: 50},
		{name: "at max", limit: 100, want: 100},
		{name: "above max clamped", limit: 200, want: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := search.SearchQuery{Limit: tt.limit}
			if got := q.EffectiveLimit(); got != tt.want {
				t.Fatalf("EffectiveLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFacetValue(t *testing.T) {
	fv := search.FacetValue{Value: "red", Count: 10}
	if fv.Value != "red" {
		t.Fatalf("Value = %q, want %q", fv.Value, "red")
	}
	if fv.Count != 10 {
		t.Fatalf("Count = %d, want %d", fv.Count, 10)
	}
}

func TestProduct(t *testing.T) {
	p := search.Product{
		ID:          "p1",
		Name:        "Running Shoes",
		Slug:        "running-shoes",
		Description: "Great shoes",
		Attributes:  map[string]interface{}{"color": "red"},
	}
	if p.ID != "p1" {
		t.Fatalf("ID = %q, want %q", p.ID, "p1")
	}
	if p.Attributes["color"] != "red" {
		t.Fatal("expected color attribute")
	}
}

func TestSearchResult(t *testing.T) {
	r := search.SearchResult{
		Products: []search.Product{{ID: "p1", Name: "Shoe"}},
		Total:    42,
		Facets: map[string][]search.FacetValue{
			"color": {{Value: "red", Count: 10}, {Value: "blue", Count: 5}},
		},
	}
	if r.Total != 42 {
		t.Fatalf("Total = %d, want %d", r.Total, 42)
	}
	if len(r.Products) != 1 {
		t.Fatalf("Products length = %d, want 1", len(r.Products))
	}
	if len(r.Facets["color"]) != 2 {
		t.Fatalf("color facets = %d, want 2", len(r.Facets["color"]))
	}
}
