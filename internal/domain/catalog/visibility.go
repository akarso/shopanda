package catalog

import "fmt"

// VisibilityMode controls how one visibility axis is resolved (PR-1055).
// auto uses VisibilityInputs / AutoBasis; visible and hidden force the axis.
// visible on Purchasable is a sellability override (force on sale), not display-only.
type VisibilityMode string

const (
	VisibilityModeAuto    VisibilityMode = "auto"
	VisibilityModeVisible VisibilityMode = "visible"
	VisibilityModeHidden  VisibilityMode = "hidden"
)

// allVisibilityModes is the stable ordered set for IsValid / migration sync.
// Keep migrations/086_add_products_visibility_modes.sql CHECK in sync.
var allVisibilityModes = []VisibilityMode{
	VisibilityModeAuto,
	VisibilityModeVisible,
	VisibilityModeHidden,
}

// AllVisibilityModes returns every recognised mode in stable order.
func AllVisibilityModes() []VisibilityMode {
	out := make([]VisibilityMode, len(allVisibilityModes))
	copy(out, allVisibilityModes)
	return out
}

// IsValid reports whether m is a recognised visibility mode.
func (m VisibilityMode) IsValid() bool {
	for _, allowed := range allVisibilityModes {
		if m == allowed {
			return true
		}
	}
	return false
}

// InvalidVisibilityModeMessage is the canonical validation text for a bad mode.
func InvalidVisibilityModeMessage(m VisibilityMode) string {
	return fmt.Sprintf("invalid visibility mode %q", m)
}

// VisibilityAxes holds the persisted per-axis override modes on a product.
type VisibilityAxes struct {
	Catalog      VisibilityMode
	Search       VisibilityMode
	Individually VisibilityMode
	Purchasable  VisibilityMode
}

// DefaultVisibilityAxes returns all axes in auto mode (merchant default).
func DefaultVisibilityAxes() VisibilityAxes {
	return VisibilityAxes{
		Catalog:      VisibilityModeAuto,
		Search:       VisibilityModeAuto,
		Individually: VisibilityModeAuto,
		Purchasable:  VisibilityModeAuto,
	}
}

// Validate reports whether every axis mode is recognised.
func (a VisibilityAxes) Validate() error {
	for _, m := range []VisibilityMode{a.Catalog, a.Search, a.Individually, a.Purchasable} {
		if !m.IsValid() {
			return fmt.Errorf("%s", InvalidVisibilityModeMessage(m))
		}
	}
	return nil
}

// Visibility is the resolved four-axis state after applying modes to AutoBasis.
type Visibility struct {
	VisibleInCatalog    bool
	VisibleInSearch     bool
	VisibleIndividually bool
	Purchasable         bool
}

// VisibilityInputs are request/catalog facts AutoBasis uses when an axis is auto.
// Status is not a field here — Product.Visibility always uses the receiver's Status.
//
// HasPrice means any price row exists for the product (including amount 0; see
// PR-1048). HasStoreAssignment means assigned to ≥ 1 store/view in the catalog
// (not “current request store”); request-scoped store/currency checks belong in
// later enforcement PRs if needed.
type VisibilityInputs struct {
	QuantityPositive   bool
	HasPrice           bool
	HasStoreAssignment bool
	HasCategory        bool
}

// VisibilityOptions carries config that must not be mixed with per-product facts.
type VisibilityOptions struct {
	// RequireCategory, when true, adds category assignment to AutoBasis
	// (config toggle; off by default — PR-1055).
	RequireCategory bool
}

// AutoBasis is the default rule for VisibilityModeAuto (PR-1055).
// The same basis feeds all four axes when each is in auto mode; merchants who
// need OOS/unpriced products still listed must force display axes to visible
// (and typically leave Purchasable as auto or hidden).
func AutoBasis(status Status, in VisibilityInputs, opts VisibilityOptions) bool {
	if status != StatusActive {
		return false
	}
	if !in.QuantityPositive {
		return false
	}
	if !in.HasPrice {
		return false
	}
	if !in.HasStoreAssignment {
		return false
	}
	if opts.RequireCategory && !in.HasCategory {
		return false
	}
	return true
}

// ResolveAxis applies mode to the auto-computed basis.
// Only VisibilityModeAuto uses the basis. visible/hidden force; unknown/empty
// fail closed to false (hidden).
func ResolveAxis(mode VisibilityMode, auto bool) bool {
	switch mode {
	case VisibilityModeAuto:
		return auto
	case VisibilityModeVisible:
		return true
	case VisibilityModeHidden:
		return false
	default:
		return false
	}
}

// Resolve computes effective visibility for all four axes from modes, product
// status, inputs, and options.
func (a VisibilityAxes) Resolve(status Status, in VisibilityInputs, opts VisibilityOptions) Visibility {
	auto := AutoBasis(status, in, opts)
	return Visibility{
		VisibleInCatalog:    ResolveAxis(a.Catalog, auto),
		VisibleInSearch:     ResolveAxis(a.Search, auto),
		VisibleIndividually: ResolveAxis(a.Individually, auto),
		Purchasable:         ResolveAxis(a.Purchasable, auto),
	}
}
