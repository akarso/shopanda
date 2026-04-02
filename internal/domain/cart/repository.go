package cart

import "context"

// CartRepository defines persistence operations for carts.
type CartRepository interface {
	// FindByID returns a cart with its items by ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Cart, error)

	// FindActiveByCustomerID returns the active cart for a customer.
	// Returns (nil, nil) when not found.
	FindActiveByCustomerID(ctx context.Context, customerID string) (*Cart, error)

	// Save persists a cart and its items (upsert).
	Save(ctx context.Context, cart *Cart) error

	// Delete removes a cart and its items by ID.
	Delete(ctx context.Context, id string) error
}
