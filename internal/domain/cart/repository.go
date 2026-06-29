package cart

import (
	"context"
	"time"
)

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

	// FindRecoveryCandidates returns active customer carts with items stale since staleBefore
	// that have not yet received a recovery email. Only carts whose customer is active with
	// a non-empty email are returned.
	FindRecoveryCandidates(ctx context.Context, staleBefore time.Time, limit int) ([]*Cart, error)

	// MarkRecoveryEmailSent atomically claims a cart for recovery (compare-and-set on
	// recovery_email_sent_at). Returns false when already claimed or sent.
	MarkRecoveryEmailSent(ctx context.Context, cartID string, sentAt time.Time) (bool, error)
}
