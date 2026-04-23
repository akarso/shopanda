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

// TaxSeeder creates a baseline standard tax rate so pricing works on first boot
// without overwriting existing operator-configured rates.
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
	st, err := storeRepo.FindDefault(ctx)
	if err != nil {
		return err
	}
	if st != nil && st.Country != "" {
		country = st.Country
	} else {
		ctxFields := map[string]interface{}{
			"country": country,
			reason":  "default store not found",
		}
		if st != nil {
			ctxFields["reason"] = "default store has empty country"
			ctxFields["store_id"] = st.ID
			ctxFields["code"] = st.Code
		}
		deps.Logger.Warn("seed.tax.fallback", ctxFields)
	}

	rate, err := tax.NewTaxRate(id.New(), country, defaultTaxClass, "", defaultTaxRateBasisPts)
	if err != nil {
		return err
	}
	created, err := rateRepo.CreateIfNotExists(ctx, &rate)
	if err != nil {
		return err
	}
	if !created {
		deps.Logger.Info("seed.tax.skip", map[string]interface{}{
			"country": country,
			"class":   defaultTaxClass,
		})
		return nil
	}

	deps.Logger.Info("seed.tax.created", map[string]interface{}{
		"country": country,
		"class":   defaultTaxClass,
		"rate":    defaultTaxRateBasisPts,
	})
	return nil
}
