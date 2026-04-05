package shipping

import (
	"context"
	"time"
)

// ShipmentRepository defines persistence operations for shipments.
type ShipmentRepository interface {
	// FindByID returns a shipment by its ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Shipment, error)

	// FindByOrderID returns the shipment for a given order.
	// Returns (nil, nil) when no shipment exists for the order.
	FindByOrderID(ctx context.Context, orderID string) (*Shipment, error)

	// Create persists a new shipment.
	Create(ctx context.Context, s *Shipment) error

	// UpdateStatus updates the status, tracking_number, provider_ref,
	// and updated_at of a shipment.
	// Uses optimistic locking: prevUpdatedAt must match the row's current
	// updated_at or a conflict error is returned.
	UpdateStatus(ctx context.Context, s *Shipment, prevUpdatedAt time.Time) error
}
