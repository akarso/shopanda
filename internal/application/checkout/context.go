package checkout

import (
	"strings"

	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/order"
)

type Address struct {
	FirstName string
	LastName  string
	Street    string
	City      string
	Postcode  string
	Country   string
}

func (a Address) IsZero() bool {
	return a.FirstName == "" && a.LastName == "" && a.Street == "" && a.City == "" && a.Postcode == "" && a.Country == ""
}

func (a Address) Normalize() Address {
	a.FirstName = strings.TrimSpace(a.FirstName)
	a.LastName = strings.TrimSpace(a.LastName)
	a.Street = strings.TrimSpace(a.Street)
	a.City = strings.TrimSpace(a.City)
	a.Postcode = strings.TrimSpace(a.Postcode)
	a.Country = strings.TrimSpace(a.Country)
	return a
}

type Input struct {
	Address        Address
	ShippingMethod string
	PaymentMethod  string
}

// Context carries data through the checkout workflow.
// Each step may read and mutate this context.
type Context struct {
	CartID     string
	Cart       *cart.Cart
	CustomerID string
	Currency   string
	Input      Input
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
