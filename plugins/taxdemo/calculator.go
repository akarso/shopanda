package taxdemo

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
)

// FlatRateCalculator applies a single configured rate to every line item.
// It follows the same PricingContext.Meta contract as the core RateTableCalculator.
type FlatRateCalculator struct {
	rateBPS int
}

// NewFlatRateCalculator returns a flat-rate extapi.TaxCalculator implementation.
func NewFlatRateCalculator(rateBPS int) *FlatRateCalculator {
	if rateBPS <= 0 {
		rateBPS = DefaultFlatRateBPS
	}
	return &FlatRateCalculator{rateBPS: rateBPS}
}

// Calculate applies the configured flat rate when tax meta is present.
func (c *FlatRateCalculator) Calculate(_ context.Context, pctx extapi.PricingContext) error {
	_, hasCountry := pctx.MetaGet("tax_country")
	_, hasMode := pctx.MetaGet("tax_mode")
	if !hasCountry && !hasMode {
		return nil
	}

	country, err := metaString(pctx, "tax_country")
	if err != nil {
		return fmt.Errorf("taxdemo calculator: %w", err)
	}
	modeStr, err := metaString(pctx, "tax_mode")
	if err != nil {
		return fmt.Errorf("taxdemo calculator: %w", err)
	}
	mode := extapi.TaxMode(modeStr)
	if !mode.IsValid() {
		return fmt.Errorf("taxdemo calculator: invalid tax_mode: %q", modeStr)
	}

	rate := extapi.TaxRate{
		ID:      "taxdemo-flat",
		Country: country,
		Class:   "flat",
		Rate:    c.rateBPS,
	}

	items := pctx.Items()
	for i, item := range items {
		if item.Total.IsZero() {
			continue
		}

		taxAmount, err := extapi.CalculateTax(item.Total, rate, mode)
		if err != nil {
			return fmt.Errorf("taxdemo calculator: variant %s: %w", item.VariantID, err)
		}
		if taxAmount.IsZero() {
			continue
		}

		adj := extapi.PricingAdjustment{
			Type:        extapi.AdjustmentTax,
			Code:        AdjustmentCode,
			Description: fmt.Sprintf("Flat VAT %.2f%% (%s)", rate.Percentage(), country),
			Amount:      taxAmount,
		}

		if mode == extapi.TaxModeInclusive {
			adj.Included = true
			rebased, err := item.Total.Sub(taxAmount)
			if err != nil {
				return fmt.Errorf("taxdemo calculator: variant %s: rebase inclusive tax: %w", item.VariantID, err)
			}
			item.Total = rebased
			if err := pctx.SetItemPrice(i, item.UnitPrice, item.Total); err != nil {
				return fmt.Errorf("taxdemo calculator: variant %s: set price: %w", item.VariantID, err)
			}
		}

		if err := pctx.AddItemAdjustment(i, adj); err != nil {
			return fmt.Errorf("taxdemo calculator: variant %s: adjustment: %w", item.VariantID, err)
		}
	}

	return nil
}

func metaString(pctx extapi.PricingContext, key string) (string, error) {
	v, ok := pctx.MetaGet(key)
	if !ok {
		return "", fmt.Errorf("missing required meta key %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("meta key %q must be a string, got %T", key, v)
	}
	return s, nil
}
