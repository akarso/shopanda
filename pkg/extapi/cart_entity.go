package extapi

// CartLineView is a read-only cart line snapshot for hook handlers.
type CartLineView struct {
	VariantID string
	Quantity  int
}

// CartView is a read-only cart snapshot surfaced on the "cart" hook payload
// key (cart.add_item.*, cart.update_item.before, cart.remove_item.after,
// cart.recalculate.before, cart.validate). Public mirror of internal/domain/cart.Cart.
type CartView struct {
	ID         string
	CustomerID string
	Items      []CartLineView
}
