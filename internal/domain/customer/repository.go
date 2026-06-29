package customer

import "context"

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

	// ListCustomers returns a paginated slice of customers ordered by email.
	ListCustomers(ctx context.Context, offset, limit int) ([]Customer, error)

	// BumpTokenGeneration atomically increments the customer's token generation.
	BumpTokenGeneration(ctx context.Context, customerID string) error

	// ChangePasswordAndBumpTokenGeneration atomically updates the password hash
	// and invalidates previously issued tokens by incrementing token generation.
	ChangePasswordAndBumpTokenGeneration(ctx context.Context, customerID, passwordHash string) error

	// Delete removes a customer by ID.
	// Returns apperror.NotFound when the customer does not exist.
	Delete(ctx context.Context, id string) error
}
