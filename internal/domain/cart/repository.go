package cart

import (
	"context"
	"errors"
	"time"
)

// ErrRecoveryClaimMismatch means ClearRecoveryEmailSent matched zero rows
// (claim already cleared or a newer mark owns recovery_email_sent_at).
var ErrRecoveryClaimMismatch = errors.New("cart: recovery claim timestamp mismatch")

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

	// DeleteEmptyGuestIfUnchanged removes cartID only when it is still guest-owned,
	// has no items, and version matches. Returns true when a row was deleted.
	DeleteEmptyGuestIfUnchanged(ctx context.Context, cartID string, expectedVersion int) (bool, error)

	// FindRecoveryCandidates returns active customer carts with items stale since staleBefore
	// that have not yet received a recovery email. Only carts whose customer is active with
	// a non-empty email are returned.
	FindRecoveryCandidates(ctx context.Context, staleBefore time.Time, limit int) ([]*Cart, error)

	// MarkRecoveryEmailSent atomically claims a cart for recovery (compare-and-set on
	// recovery_email_sent_at). Returns false when already claimed or sent.
	MarkRecoveryEmailSent(ctx context.Context, cartID string, sentAt time.Time) (bool, error)

	// ClearRecoveryEmailSent clears the recovery claim written at sentAt so a
	// failed enqueue can retry (recovery_email_sent_at → NULL). Returns
	// ErrRecoveryClaimMismatch when no row matches (already cleared or a
	// newer claim owns the column).
	ClearRecoveryEmailSent(ctx context.Context, cartID string, sentAt time.Time) error
}
