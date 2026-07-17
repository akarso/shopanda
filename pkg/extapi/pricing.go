package extapi

// Canonical core pricing pipeline step names (stable v0).
const (
	PricingStepBase              = "base"
	PricingStepCatalogPromotions = "catalog_promotions"
	PricingStepCartPromotions    = "cart_promotions"
	PricingStepTax               = "tax"
	PricingStepFinalize          = "finalize"
)

var pricingStepCatalog = []string{
	PricingStepBase,
	PricingStepCatalogPromotions,
	PricingStepCartPromotions,
	PricingStepTax,
	PricingStepFinalize,
}

// PricingStepCatalog returns documented core pricing step names.
func PricingStepCatalog() []string {
	out := make([]string, len(pricingStepCatalog))
	copy(out, pricingStepCatalog)
	return out
}

// AfterPricingStep returns an after-position for a core step name or alias.
func AfterPricingStep(anchor string) string {
	return "after:" + anchor
}

// BeforePricingStep returns a before-position for a core step name or alias.
func BeforePricingStep(anchor string) string {
	return "before:" + anchor
}

// ReplacePricingStep returns a replace-position that substitutes a core step by name.
func ReplacePricingStep(step string) string {
	return "replace:" + step
}
