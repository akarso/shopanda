package adminuser

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/customer"
)

// Repository defines persistence for admin-panel user management.
type Repository interface {
	FindByID(ctx context.Context, id string) (*customer.Customer, error)
	FindByEmail(ctx context.Context, email string) (*customer.Customer, error)
	Create(ctx context.Context, c *customer.Customer) error
	ListAdminUsers(ctx context.Context, offset, limit int) ([]customer.Customer, error)
	// UpdateAdminUser atomically applies admin user changes and enforces the last-active-admin invariant.
	// When revokeSessions is true, token_generation is bumped in the same write.
	UpdateAdminUser(ctx context.Context, c *customer.Customer, priorRole customer.Role, priorStatus customer.Status, revokeSessions bool) error
	ChangePasswordAndBumpTokenGeneration(ctx context.Context, customerID, passwordHash string) error
}
