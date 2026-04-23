package seed

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

const (
	defaultStoreCode     = "default"
	defaultStoreName     = "Sklep Domyslny"
	defaultStoreCountry  = "PL"
	defaultStoreLanguage = "pl"
)

// StoreSeeder creates a default storefront context for first boot.
type StoreSeeder struct{}

func (s *StoreSeeder) Name() string { return "default-store" }

func (s *StoreSeeder) Seed(ctx context.Context, deps Deps) error {
	repo, err := postgres.NewStoreRepo(deps.DB)
	if err != nil {
		return err
	}

	existing, err := repo.FindDefault(ctx)
	if err != nil {
		return err
	}
	if existing != nil {
		deps.Logger.Info("seed.store.skip", map[string]interface{}{
			"reason": "default store already exists",
			"code":   existing.Code,
		})
		return nil
	}

	stores, err := repo.FindAll(ctx)
	if err != nil {
		return err
	}
	if len(stores) > 0 {
		deps.Logger.Info("seed.store.skip", map[string]interface{}{
			"reason": "stores already exist",
			"count":  len(stores),
		})
		return nil
	}

	st, err := store.NewDefaultStore(id.New(), defaultStoreCode, defaultStoreName, defaultCurrency, defaultStoreCountry, defaultStoreLanguage, "")
	if err != nil {
		return err
	}

	if err := repo.Create(ctx, &st); err != nil {
		return err
	}

	deps.Logger.Info("seed.store.created", map[string]interface{}{
		"code":     st.Code,
		"currency": st.Currency,
		"country":  st.Country,
		"language": st.Language,
	})
	return nil
}
