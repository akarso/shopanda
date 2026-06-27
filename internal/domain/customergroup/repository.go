package customergroup

import "context"

// Repository persists customer groups and membership.
type Repository interface {
	// List returns groups ordered by name.
	List(ctx context.Context, offset, limit int) ([]Group, error)

	// FindByID returns a group by ID. (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Group, error)

	// FindByCode returns a group by code. (nil, nil) when not found.
	FindByCode(ctx context.Context, code string) (*Group, error)

	// Save inserts or updates a group.
	Save(ctx context.Context, group *Group) error

	// AssignCustomer sets the customer's group membership (replaces any existing).
	AssignCustomer(ctx context.Context, customerID, groupID string) error

	// RemoveCustomer clears group membership for a customer.
	RemoveCustomer(ctx context.Context, customerID string) error

	// FindGroupByCustomerID returns the group for a customer. (nil, nil) when unassigned.
	FindGroupByCustomerID(ctx context.Context, customerID string) (*Group, error)
}
