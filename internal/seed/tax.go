package seed

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

const (
	defaultTaxClass        = "standard"
	defaultTaxRateBasisPts = 2300
)

// TaxSeeder creates a baseline standard tax rate so pricing works on first boot.
type TaxSeeder struct{}

func (s *TaxSeeder) Name() string { return "default-tax" }

func (s *TaxSeeder) Seed(ctx context.Context, deps Deps) error {
	storeRepo, err := postgres.NewStoreRepo(deps.DB)
	if err != nil {
		return err
	}
	rateRepo, err := postgres.NewTaxRateRepo(deps.DB)
	if err != nil {
		return err
	}

	country := defaultStoreCountry
	if st, err := storeRepo.FindDefault(ctx); err != nil {
		return err
	} else if st != nil && st.Country != "" {
		country = st.Country
	}

	existing, err := rateRepo.FindByCountryClassAndStore(ctx, country, defaultTaxClass, "")
	if err != nil {
		return err
	}
	if existing != nil {
		deps.Logger.Info("seed.tax.skip", map[string]interface{}{
			"country": country,
			"class":   defaultTaxClass,
		})
		return nil
	}

	rate, err := tax.NewTaxRate(id.New(), country, defaultTaxClass, "", defaultTaxRateBasisPts)
	if err != nil {
		return err
	}
	if err := rateRepo.Upsert(ctx, &rate); err != nil {
		return err
	}

	deps.Logger.Info("seed.tax.created", map[string]interface{}{
		"country": country,
		"class":   defaultTaxClass,
		"rate":    defaultTaxRateBasisPts,
	})
	return nil
}
