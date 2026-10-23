package catalog

import (
	"errors"
	"fmt"
	"strings"
)

// PricingMode chooses how a product's unit price is resolved.
// fixed reads the product's own price row. dynamic (bundle only) sums
// required component prices at resolution time and does not write that sum.
type PricingMode string

const (
	PricingModeFixed   PricingMode = "fixed"
	PricingModeDynamic PricingMode = "dynamic"
)

// GroupedPriceRejectedMessage is the validation text when a caller tries to
// set or resolve a price on a grouped parent. Grouped products have no parent
// price; members are priced on their own.
const GroupedPriceRejectedMessage = "grouped products cannot be priced"

// GroupedNotPurchasableMessage is the cart validation text when a grouped
// parent is added. The parent is not a sellable SKU even if a stale price
// row exists. Members are purchased on their own.
const GroupedNotPurchasableMessage = "grouped products are not purchasable"

// DynamicBundlePriceRejectedMessage is the validation text when a caller tries
// to store a price on a dynamic bundle. The sellable amount is the component
// rollup; an own row would be shown by PDP/PLP until those surfaces use
// ResolveUnitPrice.
const DynamicBundlePriceRejectedMessage = "dynamic bundle products cannot have their own price"

// DynamicPricingModeBundleOnlyMessage is the validation text when dynamic is
// sent for a product that is not a bundle.
const DynamicPricingModeBundleOnlyMessage = "pricing mode dynamic is only allowed for bundle products"

// DynamicBundleOwnPricesMessage is the validation text when a product is set
// to dynamic pricing while one of its variants still has a stored price row.
const DynamicBundleOwnPricesMessage = "remove bundle prices before enabling dynamic pricing"

// MaxBundleRequiredComponents is the maximum required-component count a
// dynamic bundle may roll up. ResolveUnitPrice rejects a larger set, and
// HasPrice is false when the required count exceeds it. Keep the visibility
// SQL in sync with this value.
const MaxBundleRequiredComponents = 50

// AllPricingModes returns every recognised pricing mode in stable order.
// Keep migrations/090_add_products_pricing_mode.sql CHECK in sync.
func AllPricingModes() []PricingMode {
	return []PricingMode{PricingModeFixed, PricingModeDynamic}
}

// IsValid returns true if m is fixed or dynamic.
func (m PricingMode) IsValid() bool {
	for _, allowed := range AllPricingModes() {
		if m == allowed {
			return true
		}
	}
	return false
}

// InvalidPricingModeMessage is the canonical validation text for an unknown mode.
func InvalidPricingModeMessage(m PricingMode) string {
	return fmt.Sprintf("invalid pricing mode %q", m)
}

// ApplyPricingMode stores mode on a bundle. present is false when the caller
// omitted the field. An explicit dynamic token on any other type is rejected.
// An omitted mode is coerced to fixed only when the product is not a bundle
// (including a type change off bundle). A bundle that omits the field keeps
// its current mode. An unrecognised token is rejected for every type.
func (p *Product) ApplyPricingMode(raw string, present bool) error {
	if p == nil {
		return errors.New("product must not be nil")
	}
	if present {
		mode := PricingMode(strings.TrimSpace(raw))
		if !mode.IsValid() {
			return errors.New(InvalidPricingModeMessage(mode))
		}
		if mode == PricingModeDynamic && p.Type != TypeBundle {
			return errors.New(DynamicPricingModeBundleOnlyMessage)
		}
		p.PricingMode = mode
		return nil
	}
	if p.Type != TypeBundle || !p.PricingMode.IsValid() {
		p.PricingMode = PricingModeFixed
	}
	return nil
}
