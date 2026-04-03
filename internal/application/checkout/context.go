package checkout

import (
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/order"
)

// Context carries data through the checkout workflow.
// Each step may read and mutate this context.
type Context struct {
	CartID     string
	Cart       *cart.Cart
	CustomerID string
	Currency   string
	Order      *order.Order
	Meta       map[string]interface{}
	Trace      []TraceEntry
}

// TraceEntry records the result of a single step execution.
type TraceEntry struct {
	Step   string
	Status string // "ok" or "error"
	Err    string // non-empty when Status == "error"
}

// NewContext creates a Context for the given cart and customer.
func NewContext(cartID, customerID, currency string) *Context {
	return &Context{
		CartID:     cartID,
		CustomerID: customerID,
		Currency:   currency,
		Meta:       make(map[string]interface{}),
	}
}

// SetMeta stores a key-value marker in the context metadata.
func (c *Context) SetMeta(key string, value interface{}) {
	c.Meta[key] = value
}

// GetMeta retrieves a metadata value. Returns (nil, false) if absent.
func (c *Context) GetMeta(key string) (interface{}, bool) {
	v, ok := c.Meta[key]
	return v, ok
}
