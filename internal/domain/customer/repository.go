package customer

import (
	"context"
	"database/sql"
)

// CustomerRepository defines persistence operations for customers.
type CustomerRepository interface {
	// FindByID returns a customer by its ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Customer, error)

	// FindByEmail returns a customer by email address.
	// Returns (nil, nil) when not found.
	FindByEmail(ctx context.Context, email string) (*Customer, error)

	// Create persists a new customer.
	Create(ctx context.Context, c *Customer) error

	// Update persists changes to an existing customer.
	Update(ctx context.Context, c *Customer) error

	// BumpTokenGeneration atomically increments the customer's token generation.
	BumpTokenGeneration(ctx context.Context, customerID string) error

	// WithTx returns a repository bound to the given transaction.
	WithTx(tx *sql.Tx) CustomerRepository
}
