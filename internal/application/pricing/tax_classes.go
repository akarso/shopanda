package pricing

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// LineTaxClass is one cart/checkout line used by EnsureTaxClasses.
type LineTaxClass struct {
	VariantID   string
	ProductType catalog.Type
}

// EnsureTaxClasses normalizes Meta["tax_classes"] (variantID → class) for
// cart recalculate and checkout ValidateCartStep.
//
// Existing keys are preserved (hooks / explicit overrides). Missing keys are
// filled from catalog.TaxClassCode(ProductType). Empty-string overrides and
// unknown class codes are rejected. map[string]interface{} with string values
// is coerced; any other type is an error.
func EnsureTaxClasses(meta map[string]interface{}, lines []LineTaxClass) error {
	if meta == nil {
		return apperror.Validation("tax_classes: meta must not be nil")
	}
	classes, err := coerceTaxClasses(meta["tax_classes"])
	if err != nil {
		return err
	}
	if classes == nil {
		classes = make(map[string]string, len(lines))
	}
	for vid, class := range classes {
		if class == "" {
			return apperror.Validation(fmt.Sprintf(
				"tax_classes: empty class for variant %q is not a valid override", vid,
			))
		}
		if !tax.IsKnownClass(class) {
			return apperror.Validation(fmt.Sprintf(
				"tax_classes: unknown class %q for variant %q", class, vid,
			))
		}
	}
	for _, line := range lines {
		if line.VariantID == "" {
			continue
		}
		if _, ok := classes[line.VariantID]; ok {
			continue
		}
		classes[line.VariantID] = catalog.TaxClassCode(line.ProductType)
	}
	meta["tax_classes"] = classes
	return nil
}

// TaxClassesFromMeta returns the coerced tax_classes map when present and
// well-typed, or nil when absent / unreadable. Callers use this to skip
// catalog lookups for variants that already have an override; validation
// still happens in EnsureTaxClasses.
func TaxClassesFromMeta(meta map[string]interface{}) map[string]string {
	if meta == nil {
		return nil
	}
	classes, err := coerceTaxClasses(meta["tax_classes"])
	if err != nil || classes == nil {
		return nil
	}
	return classes
}

func coerceTaxClasses(raw interface{}) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case map[string]string:
		out := make(map[string]string, len(v))
		for k, val := range v {
			out[k] = val
		}
		return out, nil
	case map[string]interface{}:
		out := make(map[string]string, len(v))
		for k, val := range v {
			s, ok := val.(string)
			if !ok {
				return nil, apperror.Validation(fmt.Sprintf(
					"tax_classes: value for variant %q must be a string, got %T", k, val,
				))
			}
			out[k] = s
		}
		return out, nil
	default:
		return nil, apperror.Validation(fmt.Sprintf(
			"tax_classes: must be map[string]string, got %T", raw,
		))
	}
}
