package extapi

import "context"

// Address is a shipping/billing address captured at checkout.
type Address struct {
	FirstName string
	LastName  string
	Street    string
	City      string
	Postcode  string
	Country   string
}

// CheckoutContext is the mutable view plugins use in checkout workflow steps.
type CheckoutContext interface {
	CartID() string
	CustomerID() string
	Currency() string
	CartLoaded() bool
	CartTotalQuantity() int
	CartItemCount() int

	// Address always returns the zero value today. Every step on the
	// workflow would otherwise see the full shipping/billing address on
	// every invocation regardless of whether that step needs PII, so the
	// public SDK does not expose it yet pending a scoped, trust-gated grant
	// (reserved for a future release).
	Address() Address
	// ContactEmail always returns "" today. See Address.
	ContactEmail() string
	// ShippingMethod returns the selected shipping method code, or "" if
	// none has been selected yet (a step running before selection sees "").
	ShippingMethod() string
	// PaymentMethod returns the selected payment method code, or "" if
	// none has been selected yet.
	PaymentMethod() string
	// OrderID returns the ID of the order created by this checkout, or ""
	// if the create-order step has not run yet (most steps run before it;
	// only steps positioned "after:create_order" see a non-empty value).
	OrderID() string

	MetaGet(key string) (interface{}, bool)
	MetaSet(key string, value interface{})
}

// CheckoutStep is the public checkout workflow step contract.
type CheckoutStep interface {
	Name() string
	Execute(ctx context.Context, cctx CheckoutContext) error
}
