package pricing

import (
	"context"
	"fmt"

	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/tax"
)

// RateTableCalculator applies per-item tax using country/class rate tables.
// This is the core default tax.Calculator implementation.
//
// Required Meta keys on PricingContext when tax calculation is enabled:
//
//	"tax_country" (string): ISO 3166-1 alpha-2 country code
//	"tax_mode"    (string): "exclusive" or "inclusive"
//
// Optional Meta keys:
//
//	"store_id"    (string): store scope for rate lookup (empty = global)
//	"tax_classes" (map[string]string): variant ID → tax class override
//
// When both tax meta keys are absent, Calculate is a no-op. When one key is
// present without the other, Calculate returns an error.
type RateTableCalculator struct {
	rates        tax.RateRepository
	defaultClass string
}

// NewRateTableTaxCalculator returns the core default tax calculator.
func NewRateTableTaxCalculator(rates tax.RateRepository, defaultClass string) *RateTableCalculator {
	return &RateTableCalculator{rates: rates, defaultClass: defaultClass}
}

// Calculate applies tax for every item in the pricing context.
func (c *RateTableCalculator) Calculate(ctx context.Context, pctx *domain.PricingContext) error {
	_, hasCountry := pctx.Meta["tax_country"]
	_, hasMode := pctx.Meta["tax_mode"]
	if !hasCountry && !hasMode {
		return nil
	}

	country, err := metaString(pctx.Meta, "tax_country")
	if err != nil {
		return fmt.Errorf("tax calculator: %w", err)
	}
	modeStr, err := metaString(pctx.Meta, "tax_mode")
	if err != nil {
		return fmt.Errorf("tax calculator: %w", err)
	}
	mode := tax.TaxMode(modeStr)
	if !mode.IsValid() {
		return fmt.Errorf("tax calculator: invalid tax_mode: %q", modeStr)
	}

	storeID, err := optionalMetaString(pctx.Meta, "store_id")
	if err != nil {
		return fmt.Errorf("tax calculator: %w", err)
	}

	classes, _ := pctx.Meta["tax_classes"].(map[string]string)

	for i := range pctx.Items {
		item := &pctx.Items[i]

		class := c.defaultClass
		if classes != nil {
			if cls, ok := classes[item.VariantID]; ok {
				class = cls
			}
		}

		rate, err := c.rates.FindByCountryClassAndStore(ctx, country, class, storeID)
		if err != nil {
			return fmt.Errorf("tax calculator: variant %s: %w", item.VariantID, err)
		}
		if rate == nil && storeID != "" {
			rate, err = c.rates.FindByCountryClassAndStore(ctx, country, class, "")
			if err != nil {
				return fmt.Errorf("tax calculator: variant %s (fallback): %w", item.VariantID, err)
			}
		}
		if rate == nil || rate.Rate == 0 {
			continue
		}

		taxAmount, err := tax.Calculate(item.Total, *rate, mode)
		if err != nil {
			return fmt.Errorf("tax calculator: variant %s: %w", item.VariantID, err)
		}

		adj, err := domain.NewAdjustment(domain.AdjustmentTax, "tax."+rate.Country+"."+rate.Class, taxAmount)
		if err != nil {
			return fmt.Errorf("tax calculator: variant %s: adjustment: %w", item.VariantID, err)
		}
		adj.Description = fmt.Sprintf("VAT %.2f%% (%s)", rate.Percentage(), rate.Country)

		if mode == tax.ModeInclusive {
			adj.Included = true
			item.Total = item.Total.Sub(taxAmount)
		}

		item.Adjustments = append(item.Adjustments, adj)
	}

	return nil
}

// metaString extracts a required string value from the meta map.
func metaString(meta map[string]interface{}, key string) (string, error) {
	v, ok := meta[key]
	if !ok {
		return "", fmt.Errorf("missing required meta key %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("meta key %q must be a string, got %T", key, v)
	}
	return s, nil
}

// optionalMetaString extracts an optional string from the meta map.
func optionalMetaString(meta map[string]interface{}, key string) (string, error) {
	v, ok := meta[key]
	if !ok {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("meta key %q must be a string, got %T", key, v)
	}
	return s, nil
}
