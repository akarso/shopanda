package returns

import "context"

// Repository defines persistence for return requests.
type Repository interface {
	// Save inserts a new return and its items.
	Save(ctx context.Context, ret *Return) error

	// FindByID returns a return with its items.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Return, error)

	// FindByOrderID returns all returns for an order, newest first.
	FindByOrderID(ctx context.Context, orderID string) ([]Return, error)

	// Update persists status transitions and timestamps.
	Update(ctx context.Context, ret *Return) error
}
