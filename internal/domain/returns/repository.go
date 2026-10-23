package returns

import (
	"context"
	"errors"
)

// ErrAlreadyRestocked is returned when a restock update matches no row
// because restocked_at is already set. The caller rolls the transaction
// back and treats the receive as an idempotent success.
var ErrAlreadyRestocked = errors.New("return: already restocked")

// ErrStatusConflict is returned when Update matches no row because the
// row's status is no longer StoredStatus. The in-memory transition must
// not be treated as saved.
var ErrStatusConflict = errors.New("return: status conflict")

// Repository defines persistence for return requests.
type Repository interface {
	// Save inserts a new return and its items.
	Save(ctx context.Context, ret *Return) error

	// FindByID returns a return with its items.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Return, error)

	// FindByOrderID returns all returns for an order, newest first.
	FindByOrderID(ctx context.Context, orderID string) ([]Return, error)

	// FindByCustomerID returns all returns for a customer, newest first.
	FindByCustomerID(ctx context.Context, customerID string) ([]Return, error)

	// List returns returns ordered by created_at desc with pagination.
	List(ctx context.Context, offset, limit int) ([]Return, error)

	// Update persists a status transition. The write matches StoredStatus
	// and must not clear restocked_at or refunded_at once those columns
	// are set. A row whose status no longer matches is ErrStatusConflict.
	Update(ctx context.Context, ret *Return) error
}
