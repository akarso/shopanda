package seed

import (
	"context"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
)

const defaultCurrency = "PLN"

// ConfigSeeder sets the default store configuration values.
type ConfigSeeder struct{}

func (s *ConfigSeeder) Name() string { return "store-config" }

func (s *ConfigSeeder) Seed(ctx context.Context, deps Deps) error {
	repo := postgres.NewConfigRepo(deps.DB)

	val, err := repo.Get(ctx, "default_currency")
	if err != nil {
		return err
	}
	if val != nil {
		deps.Logger.Info("seed.config.skip", map[string]interface{}{
			"key": "default_currency",
		})
		return nil
	}

	if err := repo.Set(ctx, "default_currency", defaultCurrency); err != nil {
		return err
	}

	deps.Logger.Info("seed.config.created", map[string]interface{}{
		"default_currency": defaultCurrency,
	})
	return nil
}
