package composition

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// Filter represents a facet option available in a product listing.
type Filter struct {
	Name   string
	Values []string
}

// SortOption represents a sorting choice available in a product listing.
type SortOption struct {
	Name  string
	Field string
	Dir   string // "asc" or "desc"
}

// ListingContext holds the data built up during product listing composition (PLP).
type ListingContext struct {
	Ctx context.Context
	// Products holds shared pointers into the caller's product data.
	// Steps may read or mutate the pointed-to products directly.
	Products    []*catalog.Product
	Filters     []Filter
	SortOptions []SortOption
	Blocks      []Block
	Currency    string
	Country     string
	Meta        map[string]interface{}
}

// NewListingContext creates a ListingContext for the given products.
func NewListingContext(products []*catalog.Product) *ListingContext {
	return &ListingContext{
		Ctx:         context.Background(),
		Products:    products,
		Filters:     make([]Filter, 0),
		SortOptions: make([]SortOption, 0),
		Blocks:      make([]Block, 0),
		Meta:        make(map[string]interface{}),
	}
}
