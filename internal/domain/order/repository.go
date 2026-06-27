package order

import (
	"context"
	"time"
)

// OrderRepository defines persistence operations for orders.
type OrderRepository interface {
	// FindByID returns an order with its items by ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Order, error)

	// FindByCustomerID returns all orders for a customer, newest first.
	FindByCustomerID(ctx context.Context, customerID string) ([]Order, error)

	// FindByContactEmail returns all orders with a matching contact email, newest first.
	// Used for guest order discovery and claiming. Returns empty slice if none found.
	FindByContactEmail(ctx context.Context, contactEmail string) ([]Order, error)

	// List returns a page of orders, newest first.
	List(ctx context.Context, offset, limit int) ([]Order, error)

	// Save persists an order and its items (insert-only; orders are immutable
	// except for status transitions via UpdateStatus).
	Save(ctx context.Context, order *Order) error

	// UpdateStatus updates only the status and updated_at of an existing order.
	UpdateStatus(ctx context.Context, order *Order) error

	// LinkToCustomer persists customer ownership for a previously guest order.
	// The order must already carry the new CustomerID (set via Order.LinkToCustomer).
	// Fails when the order does not exist or is already linked to a customer.
	LinkToCustomer(ctx context.Context, order *Order) error

	// LinkToCustomerByContactEmail atomically links every unclaimed guest order
	// carrying the contact email to the customer, so a multi-order claim can
	// never be partially persisted. Returns the number of orders linked.
	LinkToCustomerByContactEmail(ctx context.Context, contactEmail, customerID string, updatedAt time.Time) (int64, error)

	// ListPaidTaxSnapshots returns paid orders with a destination country in
	// [from, to). Rows are ordered by created_at then id.
	ListPaidTaxSnapshots(ctx context.Context, from, to time.Time) ([]TaxSnapshotRow, error)
}
