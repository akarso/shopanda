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
	// FacetAttributes lists product attribute codes to compute facet counts for.
	FacetAttributes []string
	Sort            string
	StoreID         string
	Currency        string
	Limit           int
	Offset          int
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
	// CategoryIDs lists every category the product is assigned to. Was a
	// single CategoryID string before PR-1037 — that silently dropped every
	// category past the first for a multi-category product in engines that
	// store category data on the product document (Meilisearch). The
	// Postgres engine never stored category data on the document at all
	// (Search resolves category filters via a live join against
	// product_categories), so it was unaffected by that bug and this field
	// is currently write-only there too.
	CategoryIDs []string
	Price       int64 // cents
	InStock     bool
	CreatedAt   time.Time
	Attributes  map[string]interface{}
}

// Category is a lightweight search-index representation of a catalog
// category. Like Product, it intentionally avoids importing the catalog
// package. Description is always empty today — catalog.Category has no
// description field — but is kept here since a search index document is a
// natural place to surface one once the catalog domain gains it.
type Category struct {
	ID          string
	Name        string
	Slug        string
	Description string
	ParentID    string
	// ProductCount is the number of products assigned directly to this
	// category — it does not aggregate descendant categories' products
	// (see CategorySource.GetByID implementations for the reasoning).
	ProductCount int
}

// SearchResult holds the outcome of a search query.
type SearchResult struct {
	Products []Product
	Total    int
	Facets   map[string][]FacetValue
}

// Suggestion is a single autocomplete result.
type Suggestion struct {
	Text string // display text (product name)
	Type string // e.g. "product"
	URL  string // e.g. "/products/sneakers-white"
}

// MaxSuggestLimit is the upper bound for autocomplete results.
const MaxSuggestLimit = 10

// DefaultSuggestLimit is applied when limit is zero.
const DefaultSuggestLimit = 5

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

	// IndexCategory adds or updates a category document in the search index.
	IndexCategory(ctx context.Context, c Category) error

	// RemoveCategory removes a category document from the search index.
	RemoveCategory(ctx context.Context, categoryID string) error

	// Search executes the given query and returns matching products.
	Search(ctx context.Context, query SearchQuery) (SearchResult, error)

	// Suggest returns autocomplete suggestions for a prefix string.
	// limit is clamped to [1, MaxSuggestLimit]; zero uses DefaultSuggestLimit.
	Suggest(ctx context.Context, prefix string, limit int) ([]Suggestion, error)
}
