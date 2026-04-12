package composition

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// ProductContext holds the data built up during product page composition (PDP).
type ProductContext struct {
	Ctx      context.Context
	Product  *catalog.Product
	Currency string
	Country  string
	Blocks   []Block
	Meta     map[string]interface{}
}

// NewProductContext creates a ProductContext for the given product.
func NewProductContext(p *catalog.Product) *ProductContext {
	return &ProductContext{
		Ctx:     context.Background(),
		Product: p,
		Blocks:  make([]Block, 0),
		Meta:    make(map[string]interface{}),
	}
}
