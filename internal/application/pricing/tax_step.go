package pricing

import (
	"context"
	"fmt"

	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/tax"
)

// TaxStep applies per-item tax based on the customer's country and the
// variant's tax class. Tax rates are looked up from the repository.
//
// Required Meta keys on PricingContext:
//
//	"tax_country" (string): ISO 3166-1 alpha-2 country code
//	"tax_mode"    (string): "exclusive" or "inclusive"
//
// Optional Meta key:
//
//	"tax_classes" (map[string]string): variant ID → tax class override
//
// When no rate is found for a variant's class+country pair, the item is
// treated as zero-rated (no adjustment added).
//
// In exclusive mode the item's Total is unchanged and tax is added via
// an Adjustment. In inclusive mode the item's Total is reduced to the
// net amount and the extracted tax is recorded as an Adjustment with
// Included=true.
type TaxStep struct {
	rates        tax.RateRepository
	defaultClass string
}

// NewTaxStep returns a TaxStep that looks up rates from rates.
// defaultClass is used when no per-variant override is present in Meta.
func NewTaxStep(rates tax.RateRepository, defaultClass string) *TaxStep {
	return &TaxStep{rates: rates, defaultClass: defaultClass}
}

func (s *TaxStep) Name() string { return "tax" }

// Apply calculates tax for every item in the pricing context.
func (s *TaxStep) Apply(ctx context.Context, pctx *domain.PricingContext) error {
	country, err := metaString(pctx.Meta, "tax_country")
	if err != nil {
		return fmt.Errorf("tax step: %w", err)
	}
	modeStr, err := metaString(pctx.Meta, "tax_mode")
	if err != nil {
		return fmt.Errorf("tax step: %w", err)
	}
	mode := tax.TaxMode(modeStr)
	if !mode.IsValid() {
		return fmt.Errorf("tax step: invalid tax_mode: %q", modeStr)
	}

	// Optional per-variant tax class overrides.
	classes, _ := pctx.Meta["tax_classes"].(map[string]string)

	for i := range pctx.Items {
		item := &pctx.Items[i]

		class := s.defaultClass
		if classes != nil {
			if c, ok := classes[item.VariantID]; ok {
				class = c
			}
		}

		rate, err := s.rates.FindByCountryAndClass(ctx, country, class)
		if err != nil {
			return fmt.Errorf("tax step: variant %s: %w", item.VariantID, err)
		}
		if rate == nil || rate.Rate == 0 {
			continue // zero-rated
		}

		taxAmount, err := tax.Calculate(item.Total, *rate, mode)
		if err != nil {
			return fmt.Errorf("tax step: variant %s: %w", item.VariantID, err)
		}

		adj, err := domain.NewAdjustment(domain.AdjustmentTax, "tax."+rate.Country+"."+rate.Class, taxAmount)
		if err != nil {
			return fmt.Errorf("tax step: variant %s: adjustment: %w", item.VariantID, err)
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
