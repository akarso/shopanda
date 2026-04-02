package inventory

import (
	"errors"
	"time"
)

// ReservationStatus represents the state of a stock reservation.
type ReservationStatus string

const (
	ReservationActive    ReservationStatus = "active"
	ReservationReleased  ReservationStatus = "released"
	ReservationConfirmed ReservationStatus = "confirmed"
)

// IsValid returns true if s is a recognised reservation status.
func (s ReservationStatus) IsValid() bool {
	switch s {
	case ReservationActive, ReservationReleased, ReservationConfirmed:
		return true
	}
	return false
}

// Reservation represents a temporary hold on inventory for a variant.
type Reservation struct {
	ID        string
	VariantID string
	Quantity  int
	Status    ReservationStatus
	ExpiresAt time.Time
	CreatedAt time.Time
}

// NewReservation creates a Reservation with validation.
func NewReservation(id, variantID string, quantity int, expiresAt time.Time) (Reservation, error) {
	if id == "" {
		return Reservation{}, errors.New("reservation: id must not be empty")
	}
	if variantID == "" {
		return Reservation{}, errors.New("reservation: variant id must not be empty")
	}
	if quantity <= 0 {
		return Reservation{}, errors.New("reservation: quantity must be positive")
	}
	if expiresAt.IsZero() {
		return Reservation{}, errors.New("reservation: expires_at must not be zero")
	}
	return Reservation{
		ID:        id,
		VariantID: variantID,
		Quantity:  quantity,
		Status:    ReservationActive,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// IsExpired returns true if the reservation has reached or passed its expiry time.
func (r Reservation) IsExpired(now time.Time) bool {
	return !now.Before(r.ExpiresAt)
}
