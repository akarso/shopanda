package catalog

import "fmt"

// VisibilityMode controls how one visibility axis is resolved (PR-1055).
// auto uses VisibilityInputs / AutoBasis; visible and hidden force the axis.
// visible on Purchasable is a sellability override (force on sale), not display-only.
// RequiresLinkedParent with an empty assignment context still forces Purchasable
// false after that override (PR-1068). Display axes are not affected.
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
// HasPrice means a price row exists for the product (including amount 0; see
// PR-1048). A dynamic bundle is the exception: HasPrice is true only when
// every required component is an active leaf with exactly one variant and a
// global price row (store_id empty), and the required count is between 1 and
// MaxBundleRequiredComponents. The bool is currency-agnostic until facts are
// store- and currency-scoped. Listing uses that coarse flag. The sell path
// calls ResolveUnitPrice with the request currency, so a global row in
// another currency cannot make AddItem succeed. A grouped parent is never
// HasPrice. VisibilityFactsRepo computes that. AutoBasis still only reads the bool.
// HasStoreAssignment means assigned to ≥ 1 store/view in the catalog (not
// “current request store”); request-scoped store/currency checks belong in
// later enforcement PRs if needed.
type VisibilityInputs struct {
	QuantityPositive   bool
	HasPrice           bool
	HasStoreAssignment bool
	HasCategory        bool
	// RequiresLinkedParent is copied from the product or visibility snapshot
	// before resolve. It is not an AutoBasis input. When true and Assignment
	// is empty, the purchasable axis is false after ResolveAxis.
	RequiresLinkedParent bool
	// Assignment is the purchase assignment for this resolve. Listing filters
	// copy AssignmentContextFrom(ctx). Cart and checkout set one line's
	// resolved assignment so lines do not share a parent. Domain callers of
	// Product.Visibility set it directly.
	Assignment AssignmentContext
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

// Stable failure codes for AutoBasisFailureCodes (admin GUI / PR-1059).
// Treat these as a public contract: callers (the admin SPA) map every code to
// a user-facing label themselves; renaming a code is a breaking change.
const (
	AutoBasisCodeStatusInactive = "status_inactive"
	// AutoBasisCodeNoStock means no variant has stock quantity > 0.
	AutoBasisCodeNoStock = "no_stock"
	AutoBasisCodeNoPrice = "no_price"
	// AutoBasisCodeNoStoreAssignment is reserved: VisibilityInputs.HasStoreAssignment
	// is currently hard-coded true by every VisibilityFactsSource implementation
	// (no product↔store assignment table exists yet), so this code cannot be
	// produced in practice today. Kept so the admin GUI's code→label map is
	// stable once that assignment model lands.
	AutoBasisCodeNoStoreAssignment = "no_store_assignment"
	AutoBasisCodeNoCategory        = "no_category"
)

// AutoBasisFailureCodes mirrors every AutoBasis check, in the same order, and
// returns every condition currently failing (not just the first — AutoBasis
// itself short-circuits on the first failure, but the admin GUI needs the
// full list so a merchant does not fix one thing and still see "hidden").
// A nil/empty result means AutoBasis(status, in, opts) would return true.
func AutoBasisFailureCodes(status Status, in VisibilityInputs, opts VisibilityOptions) []string {
	var codes []string
	if status != StatusActive {
		codes = append(codes, AutoBasisCodeStatusInactive)
	}
	codes = append(codes, AutoBasisInputFailureCodes(in, opts)...)
	return codes
}

// AutoBasisInputFailureCodes returns every AutoBasis failure code *except*
// AutoBasisCodeStatusInactive — i.e. only the VisibilityInputs/RequireCategory
// checks (quantity/price/store/category), in the same order
// AutoBasisFailureCodes uses for them. Callers combine this with a
// separately-computed status check so a live status preview (the admin SPA
// recomputing badges when the form's status select changes, before saving)
// can recompute the full failure-code list without re-fetching facts: status
// is the only AutoBasis condition that tracks the *form*, not the persisted
// product/stock/price/store/category facts.
func AutoBasisInputFailureCodes(in VisibilityInputs, opts VisibilityOptions) []string {
	var codes []string
	if !in.QuantityPositive {
		codes = append(codes, AutoBasisCodeNoStock)
	}
	if !in.HasPrice {
		codes = append(codes, AutoBasisCodeNoPrice)
	}
	if !in.HasStoreAssignment {
		codes = append(codes, AutoBasisCodeNoStoreAssignment)
	}
	if opts.RequireCategory && !in.HasCategory {
		codes = append(codes, AutoBasisCodeNoCategory)
	}
	return codes
}

// EffectiveAxis matches the public storefront gate: non-active products are
// never allowed on any surface (LoadMany omits them → deny), even if mode is
// forced visible. Resolved (ResolveAxis) alone is not storefront truth for a
// non-active product; Effective is.
func EffectiveAxis(mode VisibilityMode, status Status, auto bool) bool {
	if status != StatusActive {
		return false
	}
	return ResolveAxis(mode, auto)
}

// AxisExplanation is the admin-GUI-facing summary of how one visibility axis
// resolved (PR-1059): the stored mode, whether that mode is a merchant
// override of the computed default, the domain-level resolved boolean
// (ResolveAxis — ignores Status), the public-gate effective boolean
// (EffectiveAxis — the storefront truth), and — only when mode is auto and
// auto is false — which AutoBasis conditions are currently failing.
//
// No JSON tags: domain type, not an HTTP response shape.
// internal/interfaces/http/admin owns
// the wire-format DTO (snake_case, always-present FailureCodes) that maps
// from this struct for the GET /admin/products/{id} payload.
type AxisExplanation struct {
	Mode         VisibilityMode
	Override     bool
	Resolved     bool
	Effective    bool
	FailureCodes []string
}

// AxesExplanation is the per-axis ExplainAxis result for all four
// VisibilityAxes of one product (struct fields, not map keys).
type AxesExplanation struct {
	Catalog      AxisExplanation
	Search       AxisExplanation
	Individually AxisExplanation
	Purchasable  AxisExplanation
}

// ExplainAxis builds an AxisExplanation for one axis. Pass the same
// AutoBasisFailureCodes(...) result used to compute auto for every axis on
// the product — failure codes only surface on the explanation when mode is
// auto and auto is false (a forced axis has nothing to "explain": the
// merchant chose the outcome directly).
func ExplainAxis(mode VisibilityMode, status Status, auto bool, failureCodes []string) AxisExplanation {
	resolved := ResolveAxis(mode, auto)
	effective := EffectiveAxis(mode, status, auto)
	exp := AxisExplanation{Mode: mode, Override: mode != VisibilityModeAuto, Resolved: resolved, Effective: effective}
	if mode == VisibilityModeAuto && !auto {
		// Copy, not alias: failureCodes is the same slice shared by every
		// axis on the product (ExplainAxes passes its one AutoBasisFailureCodes
		// result to all four ExplainAxis calls). Without a copy, a caller
		// mutating one axis's FailureCodes in place (e.g. append past len,
		// or an explicit element write) could corrupt another axis's
		// FailureCodes — or the AutoBasisFailureCodes result itself — via a
		// shared backing array.
		exp.FailureCodes = append([]string(nil), failureCodes...)
	}
	return exp
}

// ExplainAxes builds the admin "why" explanation for all four axes of one
// product in one call: the shared AutoBasis boolean, the shared failure code
// list, and a per-axis AxisExplanation struct (catalog/search/individually/
// purchasable).
func ExplainAxes(axes VisibilityAxes, status Status, in VisibilityInputs, opts VisibilityOptions) (autoBasis bool, codes []string, axesExp AxesExplanation) {
	autoBasis = AutoBasis(status, in, opts)
	codes = AutoBasisFailureCodes(status, in, opts)
	axesExp = AxesExplanation{
		Catalog:      ExplainAxis(axes.Catalog, status, autoBasis, codes),
		Search:       ExplainAxis(axes.Search, status, autoBasis, codes),
		Individually: ExplainAxis(axes.Individually, status, autoBasis, codes),
		Purchasable:  explainPurchasableAxis(axes.Purchasable, status, autoBasis, codes, in),
	}
	return
}

// explainPurchasableAxis is ExplainAxis plus the RequiresLinkedParent gate.
// The extra failure code is attached only to this axis. codes (the shared
// AutoBasis list) is not modified.
func explainPurchasableAxis(mode VisibilityMode, status Status, auto bool, failureCodes []string, in VisibilityInputs) AxisExplanation {
	exp := ExplainAxis(mode, status, auto, failureCodes)
	if !linkedParentBlocksPurchase(in) {
		return exp
	}
	exp.Resolved = false
	exp.Effective = false
	exp.FailureCodes = append(exp.FailureCodes, RequiresLinkedParentCode)
	return exp
}

// linkedParentBlocksPurchase reports the purchasable-axis override.
func linkedParentBlocksPurchase(in VisibilityInputs) bool {
	return LinkedParentBlocksPurchase(&Product{RequiresLinkedParent: in.RequiresLinkedParent}, in.Assignment)
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
	purchasable := ResolveAxis(a.Purchasable, auto)
	if linkedParentBlocksPurchase(in) {
		purchasable = false
	}
	return Visibility{
		VisibleInCatalog:    ResolveAxis(a.Catalog, auto),
		VisibleInSearch:     ResolveAxis(a.Search, auto),
		VisibleIndividually: ResolveAxis(a.Individually, auto),
		Purchasable:         purchasable,
	}
}
