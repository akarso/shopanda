package seed

import (
	"context"
	"os"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/password"
)

const (
	adminEmail           = "admin@example.com"
	adminPasswordEnvKey  = "SHOPANDA_SEED_ADMIN_PASSWORD"
	adminPasswordDefault = "admin123"
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

	adminPwd := os.Getenv(adminPasswordEnvKey)
	if adminPwd == "" {
		adminPwd = adminPasswordDefault
		deps.Logger.Warn("seed.admin.insecure_password", map[string]interface{}{
			"hint": "set " + adminPasswordEnvKey + " for a secure password",
		})
	}

	c, err := customer.NewCustomer(id.New(), adminEmail)
	if err != nil {
		return err
	}

	hash, err := password.Hash(adminPwd)
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
		if apperror.Is(err, apperror.CodeConflict) {
			deps.Logger.Info("seed.admin.skip", map[string]interface{}{
				"email": adminEmail,
			})
			return nil
		}
		return err
	}

	deps.Logger.Info("seed.admin.created", map[string]interface{}{
		"email": adminEmail,
	})
	return nil
}
