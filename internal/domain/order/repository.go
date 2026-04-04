package order

import "context"

// OrderRepository defines persistence operations for orders.
type OrderRepository interface {
	// FindByID returns an order with its items by ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Order, error)

	// FindByCustomerID returns all orders for a customer, newest first.
	FindByCustomerID(ctx context.Context, customerID string) ([]Order, error)

	// List returns a page of orders, newest first.
	List(ctx context.Context, offset, limit int) ([]Order, error)

	// Save persists an order and its items (insert-only; orders are immutable
	// except for status transitions via UpdateStatus).
	Save(ctx context.Context, order *Order) error

	// UpdateStatus updates only the status and updated_at of an existing order.
	UpdateStatus(ctx context.Context, order *Order) error
}
