package inventory

import (
	"context"
	"time"
)

// ReservationRepository defines persistence operations for inventory reservations.
type ReservationRepository interface {
	// Reserve atomically decrements stock and creates a reservation.
	// Returns an error if insufficient stock is available.
	Reserve(ctx context.Context, reservation *Reservation) error

	// Release cancels an active reservation and restores the reserved quantity to stock.
	// Returns an error if the reservation is not found or not active.
	Release(ctx context.Context, reservationID string) error

	// Confirm marks a reservation as confirmed without restoring stock
	// (stock was already decremented at reserve time).
	// Returns an error if the reservation is not found or not active.
	Confirm(ctx context.Context, reservationID string) error

	// FindByID returns a reservation by its ID.
	// Returns (nil, nil) when no reservation exists.
	FindByID(ctx context.Context, id string) (*Reservation, error)

	// ListActiveByVariantID returns all active reservations for a variant.
	ListActiveByVariantID(ctx context.Context, variantID string) ([]Reservation, error)

	// ReleaseExpiredBefore atomically releases all active reservations that expired
	// before cutoff, restoring their quantities to stock. Returns the number of
	// reservations released.
	ReleaseExpiredBefore(ctx context.Context, cutoff time.Time) (int, error)
}
