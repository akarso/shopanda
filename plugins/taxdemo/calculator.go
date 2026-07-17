package taxdemo

import (
	"context"
	"fmt"

	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/tax"
)

// FlatRateCalculator applies a single configured rate to every line item.
// It follows the same PricingContext.Meta contract as the core RateTableCalculator.
type FlatRateCalculator struct {
	rateBPS int
}

// NewFlatRateCalculator returns a flat-rate tax.Calculator implementation.
func NewFlatRateCalculator(rateBPS int) *FlatRateCalculator {
	if rateBPS <= 0 {
		rateBPS = DefaultFlatRateBPS
	}
	return &FlatRateCalculator{rateBPS: rateBPS}
}

// Calculate applies the configured flat rate when tax meta is present.
func (c *FlatRateCalculator) Calculate(_ context.Context, pctx *domainpricing.PricingContext) error {
	_, hasCountry := pctx.Meta["tax_country"]
	_, hasMode := pctx.Meta["tax_mode"]
	if !hasCountry && !hasMode {
		return nil
	}

	country, err := metaString(pctx.Meta, "tax_country")
	if err != nil {
		return fmt.Errorf("taxdemo calculator: %w", err)
	}
	modeStr, err := metaString(pctx.Meta, "tax_mode")
	if err != nil {
		return fmt.Errorf("taxdemo calculator: %w", err)
	}
	mode := tax.TaxMode(modeStr)
	if !mode.IsValid() {
		return fmt.Errorf("taxdemo calculator: invalid tax_mode: %q", modeStr)
	}

	rate := tax.TaxRate{
		ID:      "taxdemo-flat",
		Country: country,
		Class:   "flat",
		Rate:    c.rateBPS,
	}

	for i := range pctx.Items {
		item := &pctx.Items[i]
		if item.Total.IsZero() {
			continue
		}

		taxAmount, err := tax.Calculate(item.Total, rate, mode)
		if err != nil {
			return fmt.Errorf("taxdemo calculator: variant %s: %w", item.VariantID, err)
		}
		if taxAmount.IsZero() {
			continue
		}

		adj, err := domainpricing.NewAdjustment(domainpricing.AdjustmentTax, AdjustmentCode, taxAmount)
		if err != nil {
			return fmt.Errorf("taxdemo calculator: variant %s: adjustment: %w", item.VariantID, err)
		}
		adj.Description = fmt.Sprintf("Flat VAT %.2f%% (%s)", rate.Percentage(), country)

		if mode == tax.ModeInclusive {
			adj.Included = true
			item.Total = item.Total.Sub(taxAmount)
		}

		item.Adjustments = append(item.Adjustments, adj)
	}

	return nil
}

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
