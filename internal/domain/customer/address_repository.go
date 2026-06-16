package customer

import "context"

// AddressRepository defines persistence operations for saved customer addresses.
type AddressRepository interface {
	// ListByCustomer returns the customer's saved addresses, default first.
	ListByCustomer(ctx context.Context, customerID string) ([]Address, error)

	// FindByID returns an address by its ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Address, error)

	// FindDefault returns the customer's default address.
	// Returns (nil, nil) when the customer has no default.
	FindDefault(ctx context.Context, customerID string) (*Address, error)

	// Create persists a new address. The first address a customer saves, or any
	// address created with IsDefault set, becomes their default.
	Create(ctx context.Context, a *Address) error

	// Update persists changes to an existing address. When IsDefault is set the
	// address becomes the customer's default, clearing any previous default.
	Update(ctx context.Context, a *Address) error

	// SetDefault marks one address as the customer's default and clears others.
	// Returns apperror.NotFound when the address does not belong to the customer.
	SetDefault(ctx context.Context, customerID, addressID string) error

	// Delete removes an address owned by the customer.
	// Returns apperror.NotFound when the address does not belong to the customer.
	Delete(ctx context.Context, customerID, addressID string) error
}
