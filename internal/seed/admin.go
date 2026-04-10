package seed

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/password"
)

const (
	adminEmail    = "admin@example.com"
	adminPassword = "admin123"
)

// AdminSeeder creates the default admin user account.
type AdminSeeder struct{}

func (s *AdminSeeder) Name() string { return "admin-user" }

func (s *AdminSeeder) Seed(ctx context.Context, deps Deps) error {
	repo := postgres.NewCustomerRepo(deps.DB)

	existing, err := repo.FindByEmail(ctx, adminEmail)
	if err != nil {
		return err
	}
	if existing != nil {
		deps.Logger.Info("seed.admin.skip", map[string]interface{}{
			"email": adminEmail,
		})
		return nil
	}

	c, err := customer.NewCustomer(id.New(), adminEmail)
	if err != nil {
		return err
	}

	hash, err := password.Hash(adminPassword)
	if err != nil {
		return err
	}
	if err := c.SetPassword(hash); err != nil {
		return err
	}

	c.Role = customer.RoleAdmin
	c.FirstName = "Admin"
	c.LastName = "User"

	if err := repo.Create(ctx, &c); err != nil {
		return err
	}

	deps.Logger.Info("seed.admin.created", map[string]interface{}{
		"email": adminEmail,
	})
	return nil
}
