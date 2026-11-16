package extapi

// CartLineExtension is one stored extension value on a cart line.
// Plugin-only fields owned by other plugins are omitted from the view
// entirely (not included with a redacted sentinel).
type CartLineExtension struct {
	Code  string
	Value interface{}
}

// CartLineView is a read-only cart line snapshot for hook handlers.
type CartLineView struct {
	VariantID  string
	Quantity   int
	Extensions []CartLineExtension
}

// CartView is a read-only cart snapshot surfaced on the "cart" hook payload
// key (cart.add_item.*, cart.update_item.before, cart.remove_item.after,
// cart.recalculate.before, cart.validate) and via CartPort.Get / Ensure*.
// Public mirror of internal/domain/cart.Cart.
type CartView struct {
	ID         string
	CustomerID string
	Items      []CartLineView
}
