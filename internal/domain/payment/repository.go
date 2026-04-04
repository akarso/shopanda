package payment

import "context"

// PaymentRepository defines persistence operations for payments.
type PaymentRepository interface {
	// FindByID returns a payment by its ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Payment, error)

	// FindByOrderID returns the payment for a given order.
	// Returns (nil, nil) when no payment exists for the order.
	FindByOrderID(ctx context.Context, orderID string) (*Payment, error)

	// Create persists a new payment.
	Create(ctx context.Context, p *Payment) error

	// UpdateStatus updates the status, provider_ref, and updated_at of a payment.
	UpdateStatus(ctx context.Context, p *Payment) error
}
