package legal

import "context"

// ConsentRepository defines persistence operations for cookie consent.
type ConsentRepository interface {
	// FindByCustomerID returns the consent for a customer.
	// Returns (nil, nil) when not found.
	FindByCustomerID(ctx context.Context, customerID string) (*Consent, error)

	// Upsert creates or updates a consent record.
	Upsert(ctx context.Context, c *Consent) error

	// DeleteByCustomerID removes the consent record for a customer.
	DeleteByCustomerID(ctx context.Context, customerID string) error
}
