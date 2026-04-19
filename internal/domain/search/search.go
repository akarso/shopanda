package search

import (
	"context"
	"errors"
	"time"
)

// maxSearchLimit is the upper bound for results per query.
const maxSearchLimit = 100

// defaultSearchLimit is applied when Limit is zero.
const defaultSearchLimit = 20

// SearchQuery describes a product search request.
type SearchQuery struct {
	Text    string
	Filters map[string]interface{}
	Sort    string
	Limit   int
	Offset  int
}

// Validate checks that the query is well-formed.
func (q SearchQuery) Validate() error {
	if q.Offset < 0 {
		return errors.New("offset must not be negative")
	}
	if q.Limit < 0 {
		return errors.New("limit must not be negative")
	}
	return nil
}

// EffectiveLimit returns the limit clamped to [1, maxSearchLimit].
// A zero Limit is treated as defaultSearchLimit.
func (q SearchQuery) EffectiveLimit() int {
	if q.Limit <= 0 {
		return defaultSearchLimit
	}
	if q.Limit > maxSearchLimit {
		return maxSearchLimit
	}
	return q.Limit
}

// FacetValue is a single value within a facet bucket.
type FacetValue struct {
	Value string
	Count int
}

// Product is a lightweight search-result representation of a catalog product.
// It intentionally avoids importing the catalog package.
type Product struct {
	ID          string
	Name        string
	Slug        string
	Description string
	CategoryID  string
	Price       int64 // cents
	InStock     bool
	CreatedAt   time.Time
	Attributes  map[string]interface{}
}

// SearchResult holds the outcome of a search query.
type SearchResult struct {
	Products []Product
	Total    int
	Facets   map[string][]FacetValue
}

// SearchEngine is the port for product search backends.
// Implementations range from Postgres full-text search to external engines
// like Meilisearch.
type SearchEngine interface {
	// Name returns a human-readable identifier for the engine (e.g. "postgres").
	Name() string

	// IndexProduct adds or updates a product in the search index.
	IndexProduct(ctx context.Context, p Product) error

	// RemoveProduct removes a product from the search index.
	RemoveProduct(ctx context.Context, productID string) error

	// Search executes the given query and returns matching products.
	Search(ctx context.Context, query SearchQuery) (SearchResult, error)
}
