package composition

import "github.com/akarso/shopanda/internal/domain/catalog"

// ProductContext holds the data built up during product page composition (PDP).
type ProductContext struct {
	Product  *catalog.Product
	Currency string
	Country  string
	Blocks   []Block
	Meta     map[string]interface{}
}

// NewProductContext creates a ProductContext for the given product.
func NewProductContext(p *catalog.Product) *ProductContext {
	return &ProductContext{
		Product: p,
		Blocks:  make([]Block, 0),
		Meta:    make(map[string]interface{}),
	}
}
