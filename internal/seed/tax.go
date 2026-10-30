package seed

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

const defaultTaxRateBasisPts = 2300

// TaxSeeder creates baseline standard tax rates and backfills a matching
// digital rate for every existing non-zero standard row (same country/store/bps,
// CreateIfNotExists — never clobbers an operator-tuned digital row).
// Migration 108 performs the same backfill on shopanda migrate; seed is a
// second idempotent pass for environments that seed after migrate.
// Uses tax.RateRepository (ListByClass + CreateIfNotExists) — not a postgres-only API.
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
	var rates tax.RateRepository = rateRepo

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
			"reason":  "default store not found",
		}
		if st != nil {
			ctxFields["reason"] = "default store has empty country"
			ctxFields["store_id"] = st.ID
			ctxFields["code"] = st.Code
		}
		deps.Logger.Warn("seed.tax.fallback", ctxFields)
	}

	// Ensure at least one standard rate for the default-store country.
	std, err := tax.NewTaxRate(id.New(), country, tax.TaxClassStandard, "", defaultTaxRateBasisPts)
	if err != nil {
		return err
	}
	created, err := rates.CreateIfNotExists(ctx, &std)
	if err != nil {
		return err
	}
	if created {
		deps.Logger.Info("seed.tax.created", map[string]interface{}{
			"country": country,
			"class":   tax.TaxClassStandard,
			"rate":    defaultTaxRateBasisPts,
		})
	} else {
		deps.Logger.Info("seed.tax.skip", map[string]interface{}{
			"country": country,
			"class":   tax.TaxClassStandard,
		})
	}

	return backfillDigitalFromStandard(ctx, rates, deps.Logger)
}

// backfillDigitalFromStandard CreateIfNotExists a digital row for every
// non-zero standard rate (any country/store), copying basis points. Existing
// digital rows are left untouched. Zero-bps standard is skipped — intentional
// 0% VAT uses class "zero"; digital Rate==0 is refused by the calculator.
// This is a second idempotent pass after migration 108.
func backfillDigitalFromStandard(ctx context.Context, rates tax.RateRepository, log logger.Logger) error {
	standards, err := rates.ListByClass(ctx, tax.TaxClassStandard)
	if err != nil {
		return err
	}
	for _, std := range standards {
		if std.Rate == 0 {
			log.Info("seed.tax.skip", map[string]interface{}{
				"country":  std.Country,
				"class":    tax.TaxClassDigital,
				"store_id": std.StoreID,
				"reason":   "zero_bps_standard",
			})
			continue
		}
		digital, err := tax.NewTaxRate(id.New(), std.Country, tax.TaxClassDigital, std.StoreID, std.Rate)
		if err != nil {
			return err
		}
		created, err := rates.CreateIfNotExists(ctx, &digital)
		if err != nil {
			return err
		}
		if created {
			log.Info("seed.tax.created", map[string]interface{}{
				"country":  std.Country,
				"class":    tax.TaxClassDigital,
				"store_id": std.StoreID,
				"rate":     std.Rate,
			})
			continue
		}
		log.Info("seed.tax.skip", map[string]interface{}{
			"country":  std.Country,
			"class":    tax.TaxClassDigital,
			"store_id": std.StoreID,
		})
	}
	return nil
}
